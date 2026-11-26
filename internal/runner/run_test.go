package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"netbench/internal/scenario"
)

func TestParseIperf3(t *testing.T) {
	data, err := os.ReadFile("testdata/iperf3_p8.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseIperf3(data)
	if err != nil {
		t.Fatal(err)
	}
	if r.ThroughputBps < 9.9e8 || r.ThroughputBps > 9.95e8 || r.Retransmits != 0 {
		t.Errorf("throughput/retransmits: %+v", r)
	}
	if r.LatMeanMs == nil || *r.LatMeanMs < 7.3 || *r.LatMeanMs > 7.4 {
		t.Errorf("mean rtt: %v", r.LatMeanMs)
	}
	if r.LatP99Ms != nil {
		t.Error("iperf3 does not measure p99")
	}
}

func TestParseIperf3Errors(t *testing.T) {
	e, _ := os.ReadFile("testdata/iperf3_error.json")
	if _, err := ParseIperf3(e); err == nil || !strings.Contains(err.Error(), "Connection refused") {
		t.Errorf("error output: %v", err)
	}
	for name, in := range map[string]string{"empty": "", "no sum_received": `{"end":{}}`} {
		if _, err := ParseIperf3([]byte(in)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// fake records every command; the iperf3 client returns the fixture after `refuse` refused attempts.
type fake struct {
	mu      sync.Mutex
	calls   []string // "target: cmd"
	refuse  int
	fixture []byte
	errOut  []byte
}

func (f *fake) exec(_ context.Context, target, cmd string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, target+": "+cmd)
	if !strings.Contains(cmd, "iperf3 -c") {
		return nil, nil
	}
	if f.refuse > 0 {
		f.refuse--
		return f.errOut, errors.New("exit status 1")
	}
	return f.fixture, nil
}

func (f *fake) count(sub string) (n int) {
	for _, c := range f.calls {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return
}

func newFake(t *testing.T) *fake {
	fx, _ := os.ReadFile("testdata/iperf3_p8.json")
	eo, _ := os.ReadFile("testdata/iperf3_error.json")
	return &fake{fixture: fx, errOut: eo}
}

const twoFlows = `
name: t
generator: iperf3
hosts:
  h1: {ssh: ssh-h1, ip: 10.0.1.2}
  h2: {ssh: ssh-h2, ip: 10.0.2.2}
  h4: {ssh: ssh-h4, ip: 10.0.4.2}
flows:
  - {from: h1, to: h4}
  - {from: h2, to: h4}
steps:
  - {streams: 2}
  - {streams: 4}
duration: 10s
warmup: 3s
baseline_duration: 5s
`

func TestRunSteps(t *testing.T) {
	retryDelay = time.Millisecond
	sc, err := scenario.Parse([]byte(twoFlows))
	if err != nil {
		t.Fatal(err)
	}
	f, dir := newFake(t), t.TempDir()
	if err := RunSteps(context.Background(), f.exec, sc, dir); err != nil {
		t.Fatal(err)
	}
	for want, n := range map[string]int{
		"ssh-h1: iperf3 -c 10.0.4.2 -p 5201 -P 2 -t 7 -O 3 -J": 1, // load goes to the data IP, over the sender's ssh name
		"ssh-h2: iperf3 -c 10.0.4.2 -p 5202 -P 4 -t 7 -O 3 -J": 1, // second flow: own port, even into the same receiver
		"ssh-h4: iperf3 -s -1 -p 5201":                         2, // one server per flow per step
		"ssh-h4: iperf3 -s -1 -p 5202":                         2,
	} {
		if got := f.count(want); got != n {
			t.Errorf("%q called %d times, want %d: %q", want, got, n, f.calls)
		}
	}
	if n := f.count("pkill"); n != 4 {
		t.Errorf("pkill calls = %d, want 4 (2 flows x 2 steps)", n)
	}
	for _, p := range []string{
		scenario.ToolPath(dir, "01-streams-2", "h1"), scenario.ToolPath(dir, "02-streams-4", "h2"),
		filepath.Join(dir, "steps", "01-streams-2", "raw", "h1.json"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Error(err)
		}
	}
}

func TestRunStepsRetriesWhileServerStarts(t *testing.T) {
	retryDelay = time.Millisecond
	sc, _ := scenario.Parse([]byte(twoFlows))
	sc.Flows, sc.Steps = sc.Flows[:1], sc.Steps[:1]
	f := newFake(t)
	f.refuse = 2
	if err := RunSteps(context.Background(), f.exec, sc, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if n := f.count("iperf3 -c"); n != 3 {
		t.Errorf("client attempts = %d, want 3", n)
	}
}

func TestRunStepsClientFailure(t *testing.T) {
	retryDelay = time.Millisecond
	sc, _ := scenario.Parse([]byte(twoFlows))
	sc.Flows, sc.Steps = sc.Flows[:1], sc.Steps[:1]
	f := newFake(t)
	f.refuse = 100
	dir := t.TempDir()
	err := RunSteps(context.Background(), f.exec, sc, dir)
	if err == nil || !strings.Contains(err.Error(), "Connection refused") || !strings.Contains(err.Error(), "h1 -> h4") {
		t.Fatalf("error = %v", err)
	}
	if f.count("pkill") != 1 {
		t.Error("server must be cleaned up after a failed client")
	}
	if _, statErr := os.Stat(scenario.ToolPath(dir, "01-streams-2", "h1")); statErr == nil {
		t.Error("a failed step must not leave a tool result")
	}
}

func TestRunStepsWholeSeconds(t *testing.T) {
	sc, _ := scenario.Parse([]byte(twoFlows))
	sc.Duration = 1500 * time.Millisecond
	if err := RunSteps(context.Background(), newFake(t).exec, sc, t.TempDir()); err == nil {
		t.Error("fractional seconds must be rejected")
	}
}
