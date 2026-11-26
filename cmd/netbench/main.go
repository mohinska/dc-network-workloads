// Command netbench runs data center traffic profiles on a black-box network
// and reports whether the network is the bottleneck.
package main

import (
	"fmt"
	"io"
	"os"
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
	case "run", "agent", "analyze":
		fmt.Fprintf(stderr, "netbench %s: not implemented yet\n", args[0])
		return 1
	}
	fmt.Fprintf(stderr, "netbench: unknown command %q\n\n%s", args[0], usage)
	return 2
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
