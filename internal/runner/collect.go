package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"netbench/internal/collect"
	"netbench/internal/scenario"
)

// Collect starts a metric agent on every scenario host (and the controller-side infra agent, if given),
// runs f, stops the agents and stores what they recorded at dest(host). The agents also run when f fails.
// Each host pings the other end of its flow, so the probe RTT shows queueing on the loaded path.
func Collect(ctx context.Context, ex Exec, sc *scenario.Scenario, infra *collect.Agent, dest func(host string) string, f func(context.Context) error) error {
	peers := map[string]string{}
	for _, fl := range sc.Flows {
		if peers[fl.From] == "" {
			peers[fl.From] = sc.Hosts[fl.To].IP
		}
		if peers[fl.To] == "" {
			peers[fl.To] = sc.Hosts[fl.From].IP
		}
	}

	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	type agent struct{ host, pid, file string }
	var started []agent
	var startErr error
	for name, h := range sc.Hosts {
		file := "/tmp/netbench-" + tag + "-" + name + ".jsonl"
		cmd := fmt.Sprintf("command -v netbench >/dev/null || { echo 'netbench is not installed on this host' >&2; exit 1; }; "+
			"netbench agent --out %s --host %s", file, name)
		if p := peers[name]; p != "" {
			cmd += " --peer " + p
		}
		cmd += " </dev/null >/dev/null 2>>/tmp/netbench-agent.log & echo $!"
		out, err := ex(ctx, h.SSH, cmd)
		if err != nil {
			startErr = fmt.Errorf("start agent on %s: %w", name, err)
			break
		}
		started = append(started, agent{name, strings.TrimSpace(string(out)), file})
	}

	var infraBuf bytes.Buffer
	var infraDone chan struct{}
	var infraCancel context.CancelFunc = func() {}
	if infra != nil && startErr == nil {
		var ictx context.Context
		ictx, infraCancel = context.WithCancel(ctx)
		infra.Reset()
		infraDone = make(chan struct{})
		go func() { defer close(infraDone); infra.Run(ictx, &infraBuf, time.Second) }()
	}

	var fErr error
	if startErr == nil {
		fErr = f(ctx)
	}

	// Stop with a fresh context: Ctrl-C during f must still bring the samples back.
	sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var mu sync.Mutex
	errs := []error{startErr, fErr}
	var wg sync.WaitGroup
	for _, a := range started {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// TERM lets the agent flush; wait up to 5 s for it to exit, then print and delete the file.
			cmd := fmt.Sprintf("kill -TERM %s 2>/dev/null; i=0; while kill -0 %s 2>/dev/null && [ $i -lt 50 ]; do sleep 0.1; i=$((i+1)); done; cat %s; rm -f %s",
				a.pid, a.pid, a.file, a.file)
			out, err := ex(sctx, sc.Hosts[a.host].SSH, cmd)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("collect metrics from %s: %w", a.host, err))
				return
			}
			errs = append(errs, writeFile(dest(a.host), out))
		}()
	}
	wg.Wait()
	infraCancel()
	if infraDone != nil {
		<-infraDone
		errs = append(errs, writeFile(dest("_infra"), infraBuf.Bytes()))
	}
	return errors.Join(errs...)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
