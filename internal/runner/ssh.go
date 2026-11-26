// Package runner runs load generators on the testbed hosts over SSH and turns their output into scenario results.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Exec runs cmd on target (a mgmt name) and returns stdout. On failure stdout is still returned,
// because iperf3 -J reports its own errors there. Tests substitute a fake.
type Exec func(ctx context.Context, target, cmd string) ([]byte, error)

// SSH runs commands as root over the system ssh client with the testbed key.
type SSH struct{ Key string }

func (s SSH) Exec(ctx context.Context, target, cmd string) ([]byte, error) {
	var stderr bytes.Buffer
	c := exec.CommandContext(ctx, "ssh", "-i", s.Key,
		"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5", "-o", "LogLevel=ERROR", "root@"+target, cmd)
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return out, fmt.Errorf("ssh %s %q: %w: %s", target, cmd, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
