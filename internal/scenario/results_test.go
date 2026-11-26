package scenario

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sample = "../../testdata/runs/sample"

func TestLoadRunSample(t *testing.T) {
	r, err := LoadRun(sample)
	if err != nil {
		t.Fatal(err)
	}
	if r.Scenario.Name != "sample" || len(r.Steps) != 2 {
		t.Fatalf("scenario/steps: %+v", r.Scenario)
	}
	if got := r.Steps[1].Name; got != "02-streams-4" {
		t.Errorf("step name = %q", got)
	}
	tool := r.Steps[1].Tool["h1"]
	if tool.Retransmits != 365 || tool.LatMeanMs == nil || tool.LatP99Ms != nil {
		t.Errorf("tool result: %+v", tool)
	}
	if n := len(r.Steps[1].Metrics["h1"]); n != 9 {
		t.Errorf("step 2 h1 samples = %d, want 9", n)
	}
	if n := len(r.Baseline["h1"]); n != 9 {
		t.Errorf("baseline h1 samples = %d, want 9", n)
	}
	if len(r.Steps[0].Metrics["h4"]) != 0 {
		t.Error("h4 has no metrics in step 1")
	}
}

func TestLoadRunWithoutOptionalData(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(sample)); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(dir, "baseline"))
	os.RemoveAll(filepath.Join(dir, "steps", "01-streams-1", "metrics"))
	r, err := LoadRun(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Baseline) != 0 || len(r.Steps[0].Metrics) != 0 {
		t.Error("baseline and metrics must be optional")
	}
}

func TestLoadRunErrors(t *testing.T) {
	for name, mutate := range map[string]func(dir string){
		"step without tool results": func(d string) { os.RemoveAll(filepath.Join(d, "steps", "02-streams-4", "tool")) },
		"corrupt sample line": func(d string) {
			p := filepath.Join(d, "steps", "01-streams-1", "metrics", "h1.jsonl")
			os.WriteFile(p, []byte("{not json}\n"), 0o644)
		},
		"no scenario": func(d string) { os.Remove(filepath.Join(d, ScenarioFile)) },
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.CopyFS(dir, os.DirFS(sample)); err != nil {
				t.Fatal(err)
			}
			mutate(dir)
			if _, err := LoadRun(dir); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	lat := 1.5
	path := ToolPath(dir, "01-streams-1", "h1")
	if err := WriteJSON(path, ToolResult{ThroughputBps: 9e8, LatMeanMs: &lat}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "lat_p99_ms") {
		t.Error("nil p99 must be omitted")
	}
	tool, err := loadTool(filepath.Dir(path))
	if err != nil || tool["h1"].ThroughputBps != 9e8 || *tool["h1"].LatMeanMs != 1.5 {
		t.Errorf("round trip: %+v, %v", tool, err)
	}

	var buf bytes.Buffer
	in := Sample{TS: time.Date(2026, 11, 26, 18, 0, 0, 0, time.UTC), Host: "h1", Src: "snmp", Key: "RetransSegs", Value: 7}
	if err := WriteSample(&buf, in); err != nil {
		t.Fatal(err)
	}
	var out Sample
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil || out != in {
		t.Errorf("sample round trip: %+v, %v", out, err)
	}
}
