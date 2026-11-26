// Package scenario is the data contract of netbench: the scenario YAML that
// drives a run and the layout of results/<run-id>/ that every module reads
// or writes. See results.go for the results side.
package scenario

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Scenario describes one run: what to generate, between which hosts, with how many streams.
type Scenario struct {
	Name      string          `yaml:"name"`
	Generator string          `yaml:"generator"` // see generators
	Hosts     map[string]Host `yaml:"hosts"`
	Flows     []Flow          `yaml:"flows"`
	Steps     []Step          `yaml:"steps"`
	// Duration is the length of each step; Warmup is the part of it that is not measured.
	Duration         time.Duration `yaml:"duration"`
	Warmup           time.Duration `yaml:"warmup"`
	BaselineDuration time.Duration `yaml:"baseline_duration"` // metrics with no load
}

// Host is reached over SSH on the mgmt network; load and probes use IP on the data network.
type Host struct {
	SSH string `yaml:"ssh"`
	IP  string `yaml:"ip"`
}

// Flow is one sender -> receiver pair, by host name.
type Flow struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// Step is one load level of a stepped run.
type Step struct {
	Streams int `yaml:"streams"`
}

// generators known to the runner. ponytail: osu etc. are added with their runners (T13).
var generators = map[string]bool{"iperf3": true}

// Load reads and validates a scenario file.
func Load(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// Parse decodes scenario YAML (unknown fields are an error) and validates it.
func Parse(data []byte) (*Scenario, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var s Scenario
	if err := dec.Decode(&s); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("empty scenario")
		}
		return nil, err
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// Validate reports every problem found, each naming its field.
func (s *Scenario) Validate() error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if s.Name == "" {
		bad("name: required")
	}
	if !generators[s.Generator] {
		bad("generator: %q is not supported", s.Generator)
	}
	if len(s.Hosts) == 0 {
		bad("hosts: at least one required")
	}
	for name, h := range s.Hosts {
		if h.SSH == "" {
			bad("hosts.%s.ssh: required", name)
		}
		if net.ParseIP(h.IP) == nil {
			bad("hosts.%s.ip: %q is not an IP address", name, h.IP)
		}
	}
	if len(s.Flows) == 0 {
		bad("flows: at least one required")
	}
	for i, f := range s.Flows {
		for field, host := range map[string]string{"from": f.From, "to": f.To} {
			if _, ok := s.Hosts[host]; !ok {
				bad("flows[%d].%s: unknown host %q", i, field, host)
			}
		}
		if f.From == f.To {
			bad("flows[%d]: from and to are the same host %q", i, f.From)
		}
	}
	if len(s.Steps) == 0 {
		bad("steps: at least one required")
	}
	for i, st := range s.Steps {
		if st.Streams <= 0 {
			bad("steps[%d].streams: must be > 0", i)
		}
	}
	if s.Duration <= 0 {
		bad("duration: must be > 0")
	}
	if s.Warmup < 0 || (s.Duration > 0 && s.Warmup >= s.Duration) {
		bad("warmup: must be >= 0 and shorter than duration")
	}
	if s.BaselineDuration <= 0 {
		bad("baseline_duration: must be > 0")
	}
	return errors.Join(errs...)
}
