package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	for _, c := range []struct {
		args []string
		code int
		out  string // substring expected in stdout+stderr
	}{
		{nil, 2, "usage"},
		{[]string{"--help"}, 0, "analyze"},
		{[]string{"run"}, 1, "not implemented"},
		{[]string{"agent"}, 1, "not implemented"},
		{[]string{"analyze"}, 1, "not implemented"},
		{[]string{"bogus"}, 2, "unknown command"},
	} {
		var out, errb bytes.Buffer
		if got := run(c.args, &out, &errb); got != c.code {
			t.Errorf("run(%v) = %d, want %d", c.args, got, c.code)
		}
		if !strings.Contains(out.String()+errb.String(), c.out) {
			t.Errorf("run(%v) output lacks %q: %q", c.args, c.out, out.String()+errb.String())
		}
	}
}
