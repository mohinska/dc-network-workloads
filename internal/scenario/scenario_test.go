package scenario

import (
	"strings"
	"testing"
)

const valid = `
name: t
generator: iperf3
hosts:
  h1: {ssh: clab-netbench-h1, ip: 10.0.1.2}
  h4: {ssh: clab-netbench-h4, ip: 10.0.4.2}
flows:
  - {from: h1, to: h4}
steps:
  - {streams: 1}
duration: 10s
warmup: 3s
baseline_duration: 5s
`

func TestParseValid(t *testing.T) {
	s, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if s.Duration.Seconds() != 10 || s.Hosts["h4"].IP != "10.0.4.2" || s.Steps[0].Streams != 1 {
		t.Errorf("unexpected scenario: %+v", s)
	}
}

func TestLoadExample(t *testing.T) {
	if _, err := Load("../../scenarios/iperf3-basic.yaml"); err != nil {
		t.Fatal(err)
	}
}

func TestParseInvalid(t *testing.T) {
	for _, c := range []struct{ name, old, new, want string }{
		{"unknown field", "name: t", "name: t\nbogus: 1", "bogus"},
		{"no name", "name: t\n", "", "name: required"},
		{"bad generator", "generator: iperf3", "generator: nope", "generator"},
		{"no ssh", "ssh: clab-netbench-h1, ", "", "hosts.h1.ssh"},
		{"bad ip", "ip: 10.0.1.2", "ip: 10.0.1", "hosts.h1.ip"},
		{"same sender twice", "  - {from: h1, to: h4}", "  - {from: h1, to: h4}\n  - {from: h1, to: h4}", "already sends"},
		{"unknown flow host", "to: h4", "to: h9", "flows[0].to"},
		{"flow to itself", "to: h4", "to: h1", "same host"},
		{"no steps", "steps:\n  - {streams: 1}\n", "steps: []\n", "steps: at least"},
		{"zero streams", "streams: 1", "streams: 0", "steps[0].streams"},
		{"zero duration", "duration: 10s", "duration: 0s", "duration"},
		{"warmup too long", "warmup: 3s", "warmup: 10s", "warmup"},
		{"no baseline", "baseline_duration: 5s", "baseline_duration: 0s", "baseline_duration"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(valid, c.old) {
				t.Fatalf("test bug: %q not in base scenario", c.old)
			}
			_, err := Parse([]byte(strings.Replace(valid, c.old, c.new, 1)))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestParseEmpty(t *testing.T) {
	if _, err := Parse(nil); err == nil {
		t.Error("empty input must fail")
	}
}
