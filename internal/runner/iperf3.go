package runner

import (
	"encoding/json"
	"errors"
	"fmt"

	"netbench/internal/scenario"
)

type iperf3Output struct {
	Error string `json:"error"`
	End   struct {
		Streams []struct {
			Sender struct {
				MeanRTT float64 `json:"mean_rtt"` // microseconds
			} `json:"sender"`
		} `json:"streams"`
		SumSent struct {
			Retransmits int `json:"retransmits"`
		} `json:"sum_sent"`
		SumReceived *struct {
			BitsPerSecond float64 `json:"bits_per_second"`
		} `json:"sum_received"`
	} `json:"end"`
}

// iperf3Error returns the "error" text of an iperf3 -J output, or "" if there is none.
func iperf3Error(data []byte) string {
	var o iperf3Output
	if json.Unmarshal(data, &o) != nil {
		return ""
	}
	return o.Error
}

// ParseIperf3 turns `iperf3 -J` output of a TCP client into a ToolResult.
// Throughput is what the receiver got. iperf3 reports only a mean RTT, so LatP99Ms stays nil.
func ParseIperf3(data []byte) (scenario.ToolResult, error) {
	var o iperf3Output
	if err := json.Unmarshal(data, &o); err != nil {
		return scenario.ToolResult{}, fmt.Errorf("iperf3 output: %w", err)
	}
	if o.Error != "" {
		return scenario.ToolResult{}, errors.New("iperf3: " + o.Error)
	}
	if o.End.SumReceived == nil {
		return scenario.ToolResult{}, errors.New("iperf3 output has no end.sum_received")
	}
	res := scenario.ToolResult{ThroughputBps: o.End.SumReceived.BitsPerSecond, Retransmits: o.End.SumSent.Retransmits}
	var sum float64
	var n int
	for _, s := range o.End.Streams {
		if s.Sender.MeanRTT > 0 {
			sum += s.Sender.MeanRTT
			n++
		}
	}
	if n > 0 {
		ms := sum / float64(n) / 1000
		res.LatMeanMs = &ms
	}
	return res, nil
}
