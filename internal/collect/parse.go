// Package collect samples host and network state once per second and writes it as scenario.Sample lines.
// Sources are read from /proc and /sys through an fs.FS, so the parsers and the Agent are tested on saved files.
package collect

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// cpuTimes are the jiffies of one /proc/stat cpu line.
type cpuTimes struct{ total, busy, system, soft float64 }

// parseProcStat reads the "cpu" (all cores) and "cpuN" lines of /proc/stat.
func parseProcStat(text string) (map[string]cpuTimes, error) {
	out := map[string]cpuTimes{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 8 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		v := make([]float64, 8) // user nice system idle iowait irq softirq steal
		for i := range v {
			x, err := strconv.ParseFloat(f[i+1], 64)
			if err != nil {
				return nil, fmt.Errorf("/proc/stat %s: %w", f[0], err)
			}
			v[i] = x
		}
		var total float64
		for _, x := range v {
			total += x
		}
		out[f[0]] = cpuTimes{total: total, busy: total - v[3] - v[4], system: v[2], soft: v[6]}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("/proc/stat: no cpu lines")
	}
	return out, nil
}

// counterKeys are the /proc/net/snmp and /proc/net/netstat counters that are kept, by "Section.Name",
// with the key they are stored under.
var counterKeys = map[string]string{
	"Tcp.RetransSegs":          "RetransSegs",
	"Tcp.InErrs":               "InErrs",
	"Tcp.OutRsts":              "OutRsts",
	"Tcp.AttemptFails":         "AttemptFails",
	"Tcp.EstabResets":          "EstabResets",
	"Udp.InErrors":             "UdpInErrors",
	"Udp.RcvbufErrors":         "UdpRcvbufErrors",
	"Udp.SndbufErrors":         "UdpSndbufErrors",
	"TcpExt.TCPSACKReorder":    "TCPSACKReorder",
	"TcpExt.TCPDSACKRecv":      "TCPDSACKRecv",
	"TcpExt.TCPDSACKOfoRecv":   "TCPDSACKOfoRecv",
	"TcpExt.TCPTimeouts":       "TCPTimeouts",
	"TcpExt.TCPFastRetrans":    "TCPFastRetrans",
	"TcpExt.TCPLostRetransmit": "TCPLostRetransmit",
	"TcpExt.TCPSynRetrans":     "TCPSynRetrans",
}

// parseNetCounters reads the "Name: names..." / "Name: values..." line pairs of /proc/net/snmp and
// /proc/net/netstat and returns the counters listed in counterKeys.
func parseNetCounters(text string) map[string]float64 {
	out := map[string]float64{}
	lines := strings.Split(text, "\n")
	for i := 0; i+1 < len(lines); i++ {
		names, vals := strings.Fields(lines[i]), strings.Fields(lines[i+1])
		if len(names) < 2 || len(names) != len(vals) || names[0] != vals[0] {
			continue
		}
		section := strings.TrimSuffix(names[0], ":")
		for j := 1; j < len(names); j++ {
			key, ok := counterKeys[section+"."+names[j]]
			if !ok {
				continue
			}
			if x, err := strconv.ParseFloat(vals[j], 64); err == nil {
				out[key] = x
			}
		}
		i++ // the pair is consumed
	}
	return out
}

// parseSoftnet sums processed, dropped and time_squeeze over all CPUs of /proc/net/softnet_stat (hex columns 0..2).
func parseSoftnet(text string) (map[string]float64, error) {
	out := map[string]float64{"processed": 0, "dropped": 0, "time_squeeze": 0}
	cols := []string{"processed", "dropped", "time_squeeze"}
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		if len(f) < len(cols) {
			return nil, fmt.Errorf("softnet_stat: short line %q", sc.Text())
		}
		for i, k := range cols {
			x, err := strconv.ParseUint(f[i], 16, 64)
			if err != nil {
				return nil, fmt.Errorf("softnet_stat: %w", err)
			}
			out[k] += float64(x)
		}
	}
	return out, sc.Err()
}

// parseCgroupCPU reads "key value" lines of a cgroup v2 cpu.stat.
func parseCgroupCPU(text string) map[string]float64 {
	out := map[string]float64{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		if x, err := strconv.ParseFloat(f[1], 64); err == nil {
			out[f[0]] = x
		}
	}
	return out
}

var pingTime = regexp.MustCompile(`time=([0-9.]+) ms`)

// parsePing returns the RTT of a one-packet ping, and false if the packet was lost.
func parsePing(text string) (rttMs float64, ok bool) {
	m := pingTime.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	x, err := strconv.ParseFloat(m[1], 64)
	return x, err == nil
}
