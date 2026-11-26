package collect

import (
	"bufio"
	"context"
	"io"
	"io/fs"
	"os/exec"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"netbench/internal/scenario"
)

// Cgroup is a container's cgroup directory (relative to the Agent's FS root) to watch under the name Name.
type Cgroup struct{ Name, Dir string }

// Agent samples one machine. Counters are stored as per-second rates, so the first tick only records a baseline.
type Agent struct {
	Host string // label written into every sample
	Peer string // data IP to ping every tick; "" = no probe
	FS   fs.FS  // rooted at "/" (os.DirFS("/") in production)
	// Ping sends one probe; nil uses the system ping.
	Ping func(ctx context.Context, peer string) (rttMs float64, ok bool)
	// Only limits the sources (cpu, snmp, softnet, link, cgroup, probe); nil = all.
	Only []string
	// Cgroups are extra containers whose CPU is recorded (used for the controller's infra agent).
	Cgroups []Cgroup
	// Warn is called once for each source that cannot be read; nil = silent.
	Warn func(msg string)

	prev     map[string]float64
	prevCPU  map[string]cpuTimes
	prevTime time.Time
	warned   map[string]bool
}

// Reset forgets the previous tick, so the next window does not average over the gap before it.
func (a *Agent) Reset() { a.prev, a.prevCPU, a.prevTime = nil, nil, time.Time{} }

func (a *Agent) on(src string) bool { return a.Only == nil || slices.Contains(a.Only, src) }

// read returns the file's text; a missing source is reported once and skipped, never fatal.
func (a *Agent) read(name string) (string, bool) {
	data, err := fs.ReadFile(a.FS, name)
	if err != nil {
		if a.warned == nil {
			a.warned = map[string]bool{}
		}
		if !a.warned[name] && a.Warn != nil {
			a.Warn("source skipped: " + err.Error())
		}
		a.warned[name] = true
		return "", false
	}
	return string(data), true
}

// Tick takes one reading at time now.
func (a *Agent) Tick(ctx context.Context, now time.Time) []scenario.Sample {
	var out []scenario.Sample
	emit := func(src, key string, v float64) {
		out = append(out, scenario.Sample{TS: now, Host: a.Host, Src: src, Key: key, Value: v})
	}
	dt := now.Sub(a.prevTime).Seconds()
	havePrev := a.prev != nil && dt > 0
	next := map[string]float64{}
	// rate records a counter; once a previous reading exists it emits the per-second delta times scale.
	rate := func(src, key string, cur, scale float64) {
		id := src + "|" + key
		next[id] = cur
		if p, ok := a.prev[id]; ok && havePrev {
			emit(src, key, scale*max(0, (cur-p)/dt)) // a counter reset reads as 0, not negative
		}
	}

	nextCPU := map[string]cpuTimes{}
	if a.on("cpu") {
		if text, ok := a.read("proc/stat"); ok {
			if cur, err := parseProcStat(text); err == nil {
				nextCPU = cur
				if a.prevCPU != nil {
					a.emitCPU(cur, emit)
				}
			}
		}
	}
	if a.on("snmp") {
		for _, f := range []string{"proc/net/snmp", "proc/net/netstat"} {
			if text, ok := a.read(f); ok {
				for k, v := range parseNetCounters(text) {
					rate("snmp", k, v, 1)
				}
			}
		}
	}
	if a.on("softnet") {
		if text, ok := a.read("proc/net/softnet_stat"); ok {
			if m, err := parseSoftnet(text); err == nil {
				for k, v := range m {
					rate("softnet", k, v, 1)
				}
			}
		}
	}
	if a.on("link") {
		a.readLinks(rate)
	}
	if a.on("cgroup") {
		a.readCgroup("cgroup", "sys/fs/cgroup", rate)
		for _, c := range a.Cgroups {
			a.readCgroup("cgroup:"+c.Name, c.Dir, rate)
		}
	}
	if a.on("probe") && a.Peer != "" {
		rtt, ok := a.ping(ctx)
		lost := 1.0
		if ok {
			lost = 0
			emit("probe", "rtt_ms", rtt)
		}
		emit("probe", "probe_loss", lost) // ponytail: a lost probe waits up to 1 s, which stretches this tick
	}

	a.prev, a.prevCPU, a.prevTime = next, nextCPU, now
	return out
}

// emitCPU writes busy/soft/sys percent of all cores, and the busiest core's value (a core at 100 % soft is a bottleneck).
func (a *Agent) emitCPU(cur map[string]cpuTimes, emit func(src, key string, v float64)) {
	pct := func(c, p cpuTimes) (busy, soft, sys float64, ok bool) {
		d := c.total - p.total
		if d <= 0 {
			return 0, 0, 0, false
		}
		return 100 * (c.busy - p.busy) / d, 100 * (c.soft - p.soft) / d, 100 * (c.system - p.system) / d, true
	}
	if p, ok := a.prevCPU["cpu"]; ok {
		if b, s, y, ok := pct(cur["cpu"], p); ok {
			emit("cpu", "cpu_busy_pct", b)
			emit("cpu", "cpu_soft_pct", s)
			emit("cpu", "cpu_sys_pct", y)
		}
	}
	var mb, ms, my float64
	for name, c := range cur {
		if name == "cpu" {
			continue
		}
		if p, ok := a.prevCPU[name]; ok {
			if b, s, y, ok := pct(c, p); ok {
				mb, ms, my = max(mb, b), max(ms, s), max(my, y)
			}
		}
	}
	emit("cpu", "cpu_busy_pct_max", mb)
	emit("cpu", "cpu_soft_pct_max", ms)
	emit("cpu", "cpu_sys_pct_max", my)
}

// readLinks records the interface counters of every interface but lo.
func (a *Agent) readLinks(rate func(src, key string, cur, scale float64)) {
	ents, err := fs.ReadDir(a.FS, "sys/class/net")
	if err != nil {
		a.read("sys/class/net") // reports the missing source once
		return
	}
	for _, e := range ents {
		if e.Name() == "lo" {
			continue
		}
		for _, k := range []string{"rx_bytes", "tx_bytes", "rx_packets", "tx_packets", "rx_errors", "tx_errors", "rx_dropped", "tx_dropped"} {
			if text, ok := a.read(path.Join("sys/class/net", e.Name(), "statistics", k)); ok {
				if v, err := parseFloat(text); err == nil {
					rate("link:"+e.Name(), k, v, 1)
				}
			}
		}
	}
}

// readCgroup records throttling and CPU use of a cgroup. cpu_pct is the share of ONE core in percent.
func (a *Agent) readCgroup(src, dir string, rate func(src, key string, cur, scale float64)) {
	text, ok := a.read(path.Join(dir, "cpu.stat"))
	if !ok {
		return
	}
	m := parseCgroupCPU(text)
	for _, k := range []string{"nr_throttled", "throttled_usec"} {
		if v, ok := m[k]; ok {
			rate(src, k, v, 1)
		}
	}
	if v, ok := m["usage_usec"]; ok {
		rate(src, "cpu_pct", v, 1e-4) // usec per second -> percent of one core
	}
}

func (a *Agent) ping(ctx context.Context) (float64, bool) {
	if a.Ping != nil {
		return a.Ping(ctx, a.Peer)
	}
	return SystemPing(ctx, a.Peer)
}

// SystemPing sends one ICMP echo with the system ping and waits up to 1 s.
func SystemPing(ctx context.Context, peer string) (float64, bool) {
	out, _ := exec.CommandContext(ctx, "ping", "-c", "1", "-W", "1", peer).Output()
	return parsePing(string(out))
}

// Run ticks every interval until ctx is cancelled, writing JSON lines to w.
func (a *Agent) Run(ctx context.Context, w io.Writer, interval time.Duration) error {
	bw := bufio.NewWriter(w)
	tk := time.NewTicker(interval)
	defer tk.Stop()
	for {
		for _, s := range a.Tick(ctx, time.Now()) {
			if err := scenario.WriteSample(bw, s); err != nil {
				return err
			}
		}
		if err := bw.Flush(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tk.C:
		}
	}
}

func parseFloat(s string) (float64, error) { return strconv.ParseFloat(strings.TrimSpace(s), 64) }
