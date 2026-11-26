package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"netbench/internal/scenario"
)

const basePort = 5201

// retryDelay separates connection attempts while the iperf3 server starts (a var so tests can shorten it).
var retryDelay = 300 * time.Millisecond

// RunSteps runs every step of the scenario one after another; the flows of a step run in parallel.
// For each flow it writes steps/<step>/tool/<sender>.json (ToolResult) and steps/<step>/raw/<sender>.json
// (the generator's own output) under runDir.
func RunSteps(ctx context.Context, ex Exec, sc *scenario.Scenario, runDir string) error {
	if sc.Duration%time.Second != 0 || sc.Warmup%time.Second != 0 {
		return errors.New("iperf3 takes whole seconds: duration and warmup must be a whole number of seconds")
	}
	for i, st := range sc.Steps {
		step := scenario.StepName(i, st)
		sctx, cancel := context.WithTimeout(ctx, sc.Duration+20*time.Second)
		errs := make([]error, len(sc.Flows))
		var wg sync.WaitGroup
		for k, f := range sc.Flows {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[k] = runFlow(sctx, ex, sc, k, f, st.Streams, runDir, step)
			}()
		}
		wg.Wait()
		cancel()
		if err := errors.Join(errs...); err != nil {
			return fmt.Errorf("step %s: %w", step, err)
		}
	}
	return nil
}

// runFlow starts an iperf3 server on the receiver, runs the client on the sender and stores the result.
// Each flow has its own port, so several senders can target one receiver (incast).
func runFlow(ctx context.Context, ex Exec, sc *scenario.Scenario, k int, f scenario.Flow, streams int, runDir, step string) error {
	from, to := sc.Hosts[f.From], sc.Hosts[f.To]
	port := basePort + k

	srvDone := make(chan error, 1)
	go func() { _, err := ex(ctx, to.SSH, fmt.Sprintf("iperf3 -s -1 -p %d", port)); srvDone <- err }()
	defer func() { // the one-off server stays up if the client never connected; [i] keeps pkill from matching itself
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ex(cctx, to.SSH, fmt.Sprintf("pkill -f '[i]perf3 -s -1 -p %d'", port))
	}()

	cmd := fmt.Sprintf("iperf3 -c %s -p %d -P %d -t %d -O %d -J",
		to.IP, port, streams, int((sc.Duration-sc.Warmup)/time.Second), int(sc.Warmup/time.Second))
	var out []byte
	var err error
	for try := 0; try < 10; try++ {
		out, err = ex(ctx, from.SSH, cmd)
		if err == nil || !bytes.Contains(out, []byte("Connection refused")) { // server not listening yet
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s -> %s: %w", f.From, f.To, ctx.Err())
		case <-time.After(retryDelay):
		}
	}
	if err != nil {
		if msg := iperf3Error(out); msg != "" {
			err = fmt.Errorf("iperf3: %s (%w)", msg, err)
		}
		select {
		case serr := <-srvDone:
			if serr != nil {
				err = errors.Join(err, fmt.Errorf("server on %s: %w", f.To, serr))
			}
		default:
		}
		return fmt.Errorf("%s -> %s: %w", f.From, f.To, err)
	}
	res, err := ParseIperf3(out)
	if err != nil {
		return fmt.Errorf("%s -> %s: %w", f.From, f.To, err)
	}
	raw := filepath.Join(runDir, "steps", step, "raw", f.From+".json")
	if err := os.MkdirAll(filepath.Dir(raw), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(raw, out, 0o644); err != nil {
		return err
	}
	return scenario.WriteJSON(scenario.ToolPath(runDir, step, f.From), res)
}
