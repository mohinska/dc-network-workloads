package collect

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// parseDockerPS reads `docker ps --no-trunc --format '{{.ID}} {{.Names}}'` and returns the cgroup of every
// container under systemd's cgroup driver (system.slice/docker-<id>.scope), named without the lab prefix.
func parseDockerPS(text, prefix string) []Cgroup {
	var out []Cgroup
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		out = append(out, Cgroup{
			Name: strings.TrimPrefix(f[1], prefix),
			Dir:  "sys/fs/cgroup/system.slice/docker-" + f[0] + ".scope",
		})
	}
	return out
}

// NewInfra builds the controller-side agent: CPU of the Docker host and of every lab container
// (routers included), recorded under the host name "_infra". lab is the containerlab prefix, e.g. "clab-netbench-".
// ponytail: assumes the systemd cgroup driver; a missing cgroup dir is reported once and skipped.
func NewInfra(ctx context.Context, lab string, warn func(string)) *Agent {
	a := &Agent{Host: "_infra", FS: os.DirFS("/"), Only: []string{"cpu", "cgroup"}, Warn: warn}
	out, err := exec.CommandContext(ctx, "docker", "ps", "--no-trunc", "--filter", "name="+lab, "--format", "{{.ID}} {{.Names}}").Output()
	if err != nil {
		if warn != nil {
			warn(fmt.Sprintf("infra: docker ps failed, container CPU not recorded: %v", err))
		}
		return a
	}
	a.Cgroups = parseDockerPS(string(out), lab)
	return a
}
