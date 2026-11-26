// Command netbench runs data center traffic profiles on a black-box network
// and reports whether the network is the bottleneck.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"netbench/internal/runner"
	"netbench/internal/scenario"
)

const usage = `usage: netbench <command>

commands:
  run <scenario.yaml>     run a scenario (controller)
  agent                   collect host metrics (started by run)
  analyze <results/dir>   analyze saved results
`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "run":
		return runCmd(args[1:], stdout, stderr)
	case "agent", "analyze":
		fmt.Fprintf(stderr, "netbench %s: not implemented yet\n", args[0])
		return 1
	}
	fmt.Fprintf(stderr, "netbench: unknown command %q\n\n%s", args[0], usage)
	return 2
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// runCmd loads a scenario, runs its steps on the testbed and prints what each host got.
// ponytail: only the load steps for now; baseline, metric agents and analysis join in T4/T6.
func runCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	key := fs.String("key", "testbed/.ssh/id_ed25519", "SSH key for the testbed hosts")
	out := fs.String("out", "results", "directory for run results")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: netbench run [--key file] [--out dir] <scenario.yaml>")
		return 2
	}
	sc, err := scenario.Load(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "netbench:", err)
		return 1
	}
	dir := filepath.Join(*out, time.Now().Format("2006-01-02_150405"))
	raw, _ := os.ReadFile(fs.Arg(0))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(stderr, "netbench:", err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(dir, scenario.ScenarioFile), raw, 0o644); err != nil {
		fmt.Fprintln(stderr, "netbench:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Fprintf(stdout, "%s: %d steps, results in %s\n", sc.Name, len(sc.Steps), dir)
	if err := runner.RunSteps(ctx, runner.SSH{Key: *key}.Exec, sc, dir); err != nil {
		fmt.Fprintln(stderr, "netbench:", err)
		return 1
	}
	r, err := scenario.LoadRun(dir)
	if err != nil {
		fmt.Fprintln(stderr, "netbench:", err)
		return 1
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "step\tstreams\thost\tMbit/s\tretrans\tRTT ms")
	for _, st := range r.Steps {
		hosts := make([]string, 0, len(st.Tool))
		for h := range st.Tool {
			hosts = append(hosts, h)
		}
		sort.Strings(hosts)
		for _, h := range hosts {
			t, rtt := st.Tool[h], "-"
			if t.LatMeanMs != nil {
				rtt = fmt.Sprintf("%.2f", *t.LatMeanMs)
			}
			fmt.Fprintf(tw, "%s\t%d\t%s\t%.0f\t%d\t%s\n", st.Name, st.Streams, h, t.ThroughputBps/1e6, t.Retransmits, rtt)
		}
	}
	tw.Flush()
	return 0
}
