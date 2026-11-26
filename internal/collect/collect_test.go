package collect

import (
	"context"
	"os"
	"testing"
	"testing/fstest"
	"time"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseProcStat(t *testing.T) {
	m, err := parseProcStat(fixture(t, "proc_stat"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 9 { // all cores + cpu0..cpu7
		t.Fatalf("cpu lines = %d, want 9", len(m))
	}
	c := m["cpu"]
	if c.total <= c.busy || c.busy <= 0 || c.soft <= 0 || c.system <= 0 {
		t.Errorf("cpu totals: %+v", c)
	}
	if _, err := parseProcStat("junk"); err == nil {
		t.Error("no cpu lines must fail")
	}
}

func TestParseNetCounters(t *testing.T) {
	m := parseNetCounters(fixture(t, "proc_snmp"))
	if m["RetransSegs"] != 6693 || m["OutRsts"] != 96 || m["UdpInErrors"] != 0 {
		t.Errorf("snmp: %v", m)
	}
	if _, ok := m["MaxConn"]; ok {
		t.Error("only listed counters are kept")
	}
	ext := parseNetCounters(fixture(t, "proc_netstat"))
	for _, k := range []string{"TCPSACKReorder", "TCPDSACKRecv", "TCPTimeouts", "TCPFastRetrans", "TCPLostRetransmit", "TCPSynRetrans"} {
		if _, ok := ext[k]; !ok {
			t.Errorf("netstat lacks %s", k)
		}
	}
}

func TestParseSoftnet(t *testing.T) {
	m, err := parseSoftnet("0028fa0e 00000000 00000010 0\n00310513 00000002 0000000a 0\n")
	if err != nil || m["processed"] != 0x28fa0e+0x310513 || m["dropped"] != 2 || m["time_squeeze"] != 0x1a {
		t.Errorf("softnet: %v, %v", m, err)
	}
	if _, err := parseSoftnet(fixture(t, "proc_softnet_stat")); err != nil {
		t.Error(err)
	}
	if _, err := parseSoftnet("zz 0 0"); err == nil {
		t.Error("bad hex must fail")
	}
}

func TestParseCgroupAndPing(t *testing.T) {
	m := parseCgroupCPU(fixture(t, "cgroup_cpu.stat"))
	if m["usage_usec"] <= 0 || m["user_usec"]+m["system_usec"] > m["usage_usec"]*1.01 || m["nr_throttled"] != 0 {
		t.Errorf("cpu.stat: %v", m)
	}
	if rtt, ok := parsePing(fixture(t, "ping_ok.txt")); !ok || rtt != 0.068 {
		t.Errorf("ping ok: %v %v", rtt, ok)
	}
	if _, ok := parsePing(fixture(t, "ping_lost.txt")); ok {
		t.Error("a lost probe must not parse")
	}
}

func TestParseDockerPS(t *testing.T) {
	got := parseDockerPS("abc123 clab-netbench-leaf1\nbad line here\ndef456 clab-netbench-h4\n", "clab-netbench-")
	if len(got) != 2 || got[0].Name != "leaf1" || got[1].Dir != "sys/fs/cgroup/system.slice/docker-def456.scope" {
		t.Errorf("got %+v", got)
	}
}

// tick builds a file system with the given counters; cpu is "busy idle" jiffies of the one-core machine.
func machine(retrans, rxBytes, busy, idle, usec int) fstest.MapFS {
	f := func(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }
	return fstest.MapFS{
		"proc/stat":                                          f("cpu  " + itoa(busy) + " 0 0 " + itoa(idle) + " 0 0 0 0 0 0\ncpu0 " + itoa(busy) + " 0 0 " + itoa(idle) + " 0 0 0 0 0 0\n"),
		"proc/net/snmp":                                      f("Tcp: RtoAlgorithm RetransSegs\nTcp: 1 " + itoa(retrans) + "\n"),
		"sys/class/net/lo/statistics/rx_bytes":               f("999\n"),
		"sys/class/net/eth1/statistics/rx_bytes":             f(itoa(rxBytes) + "\n"),
		"sys/fs/cgroup/cpu.stat":                             f("usage_usec " + itoa(usec) + "\nnr_throttled 0\nthrottled_usec 0\n"),
		"sys/fs/cgroup/system.slice/docker-x.scope/cpu.stat": f("usage_usec " + itoa(usec*2) + "\nnr_throttled " + itoa(usec/250_000) + "\n"),
	}
}

func itoa(n int) string { return fmtInt(n) }

func TestAgentRates(t *testing.T) {
	var warns []string
	a := &Agent{
		Host: "h1", Peer: "10.0.4.2", FS: machine(100, 0, 0, 0, 0),
		Ping:    func(context.Context, string) (float64, bool) { return 0.5, true },
		Cgroups: []Cgroup{{Name: "leaf1", Dir: "sys/fs/cgroup/system.slice/docker-x.scope"}},
		Warn:    func(m string) { warns = append(warns, m) },
	}
	t0 := time.Date(2026, 11, 26, 18, 0, 0, 0, time.UTC)
	first := a.Tick(context.Background(), t0)
	for _, s := range first {
		if s.Src != "probe" {
			t.Errorf("first tick must only record a baseline, got %+v", s)
		}
	}
	a.FS = machine(110, 4000, 150, 50, 1_000_000) // 2 s later: +10 retrans, +4000 bytes, 75% busy, 1 s CPU
	got := map[string]float64{}
	for _, s := range a.Tick(context.Background(), t0.Add(2*time.Second)) {
		got[s.Src+"/"+s.Key] = s.Value
		if s.Host != "h1" || !s.TS.Equal(t0.Add(2*time.Second)) {
			t.Errorf("bad host/ts: %+v", s)
		}
	}
	for k, want := range map[string]float64{
		"snmp/RetransSegs": 5, "link:eth1/rx_bytes": 2000, "cpu/cpu_busy_pct": 75, "cpu/cpu_busy_pct_max": 75,
		"cgroup/cpu_pct": 50, "cgroup:leaf1/cpu_pct": 100, "cgroup:leaf1/nr_throttled": 2,
		"probe/rtt_ms": 0.5, "probe/probe_loss": 0,
	} {
		if g, ok := got[k]; !ok || g != want {
			t.Errorf("%s = %v (present %v), want %v", k, g, ok, want)
		}
	}
	if _, ok := got["link:lo/rx_bytes"]; ok {
		t.Error("lo must be skipped")
	}
	if len(warns) == 0 { // softnet_stat and netstat are absent in the fake machine
		t.Error("missing sources must be reported")
	}
	n := len(warns)
	a.Tick(context.Background(), t0.Add(4*time.Second))
	if len(warns) != n {
		t.Error("a missing source is reported once")
	}
}

func TestAgentCounterResetAndLostProbe(t *testing.T) {
	a := &Agent{Host: "h1", Peer: "x", FS: machine(500, 0, 0, 0, 0), Only: []string{"snmp", "probe"},
		Ping: func(context.Context, string) (float64, bool) { return 0, false }}
	t0 := time.Now()
	a.Tick(context.Background(), t0)
	a.FS = machine(3, 0, 0, 0, 0) // counter went down (reset)
	got := map[string]float64{}
	for _, s := range a.Tick(context.Background(), t0.Add(time.Second)) {
		got[s.Key] = s.Value
	}
	if got["RetransSegs"] != 0 || got["probe_loss"] != 1 {
		t.Errorf("got %v", got)
	}
	if _, ok := got["rtt_ms"]; ok {
		t.Error("no rtt for a lost probe")
	}
	if _, ok := got["cpu_busy_pct"]; ok {
		t.Error("Only must filter sources")
	}
}
