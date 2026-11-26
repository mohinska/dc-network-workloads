package scenario

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Layout of a run directory (results/<run-id>/):
//
//	scenario.yaml                        copy of the scenario
//	baseline/metrics/<host>.jsonl        samples with no load
//	steps/<NN-streams-S>/tool/<host>.json     ToolResult per sending host
//	steps/<NN-streams-S>/metrics/<host>.jsonl samples; host "_infra" = Docker host and routers
//	verdict.json, report.md, charts/     written by the analyze module
const ScenarioFile = "scenario.yaml"

// ToolResult is what a load generator reported for one host in one step.
// A nil latency means the tool does not measure it (iperf3 gives a mean RTT but no p99).
type ToolResult struct {
	ThroughputBps float64  `json:"throughput_bps"`
	LatMeanMs     *float64 `json:"lat_mean_ms,omitempty"`
	LatP99Ms      *float64 `json:"lat_p99_ms,omitempty"`
	Retransmits   int      `json:"retransmits"`
}

// Sample is one metric reading, one JSON object per line.
// Counters are stored as a per-second rate (delta since the previous sample), not as raw totals.
// Key is the original counter name (RetransSegs, cpu_busy_pct, rtt_ms); Src is the source
// (cpu, snmp, softnet, link:<if>, cgroup, probe).
type Sample struct {
	TS    time.Time `json:"ts"`
	Host  string    `json:"host"`
	Src   string    `json:"src"`
	Key   string    `json:"key"`
	Value float64   `json:"value"`
}

// StepResult is everything recorded for one step. Metrics may be empty (the rule is then "not checked").
type StepResult struct {
	Name    string // directory name, see StepName
	Streams int
	Tool    map[string]ToolResult // by host
	Metrics map[string][]Sample   // by host
}

// Run is a loaded run directory.
type Run struct {
	Dir      string
	Scenario *Scenario
	Baseline map[string][]Sample // by host; empty if there is no baseline
	Steps    []StepResult        // same order as Scenario.Steps
}

// StepName is the directory name of step i (0-based): 01-streams-4.
func StepName(i int, s Step) string { return fmt.Sprintf("%02d-streams-%d", i+1, s.Streams) }

func ToolPath(runDir, step, host string) string {
	return filepath.Join(runDir, "steps", step, "tool", host+".json")
}

func MetricsPath(runDir, step, host string) string {
	return filepath.Join(runDir, "steps", step, "metrics", host+".jsonl")
}

func BaselineMetricsPath(runDir, host string) string {
	return filepath.Join(runDir, "baseline", "metrics", host+".jsonl")
}

// WriteJSON writes v as indented JSON, creating parent directories.
func WriteJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// WriteSample appends one sample line to w.
func WriteSample(w io.Writer, s Sample) error { return json.NewEncoder(w).Encode(s) }

// LoadRun reads a run directory. Every step needs at least one tool result;
// baseline and metrics are optional.
func LoadRun(dir string) (*Run, error) {
	sc, err := Load(filepath.Join(dir, ScenarioFile))
	if err != nil {
		return nil, err
	}
	r := &Run{Dir: dir, Scenario: sc}
	if r.Baseline, err = loadSamples(filepath.Join(dir, "baseline", "metrics")); err != nil {
		return nil, err
	}
	for i, st := range sc.Steps {
		name := StepName(i, st)
		base := filepath.Join(dir, "steps", name)
		tool, err := loadTool(filepath.Join(base, "tool"))
		if err != nil {
			return nil, fmt.Errorf("step %s: %w", name, err)
		}
		metrics, err := loadSamples(filepath.Join(base, "metrics"))
		if err != nil {
			return nil, fmt.Errorf("step %s: %w", name, err)
		}
		r.Steps = append(r.Steps, StepResult{Name: name, Streams: st.Streams, Tool: tool, Metrics: metrics})
	}
	return r, nil
}

func loadTool(dir string) (map[string]ToolResult, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no tool results in %s", dir)
	}
	out := map[string]ToolResult{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var t ToolResult
		if err := json.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		out[strings.TrimSuffix(filepath.Base(f), ".json")] = t
	}
	return out, nil
}

// loadSamples reads every <host>.jsonl in dir; a missing dir is an empty result.
func loadSamples(dir string) (map[string][]Sample, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	out := map[string][]Sample{}
	for _, f := range files {
		samples, err := readSamples(f)
		if err != nil {
			return nil, err
		}
		out[strings.TrimSuffix(filepath.Base(f), ".jsonl")] = samples
	}
	return out, nil
}

func readSamples(path string) ([]Sample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Sample
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var s Sample
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		out = append(out, s)
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return out, nil
}
