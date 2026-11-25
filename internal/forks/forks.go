// Package forks loads the curated fork schedule (forks.yaml) and answers
// which fork is active, which one is next and which was postponed.
package forks

import (
	"bytes"
	_ "embed" // required for the //go:embed directive below
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ropslabs/x3f/internal/ethereum"
	"github.com/ropslabs/x3f/internal/network"
	"github.com/ropslabs/x3f/internal/releases"
)

// embedded is forks.yaml, compiled into the binary. The //go:embed
// directive makes the compiler copy the file's content into this variable,
// so x3f works without any data files next to it.
//
//go:embed forks.yaml
var embedded []byte

// Seconds per epoch on all supported networks: 32 slots of 12 seconds.
const secondsPerEpoch = 32 * 12

// Schedule is the parsed fork schedule.
type Schedule struct {
	// Updated is when the data was last checked against the sources.
	Updated  time.Time
	Networks map[string]Network
}

// Network holds the forks of one network, ordered by activation time.
type Network struct {
	Name        string
	ChainID     uint64
	GenesisTime int64
	Forks       []Fork
}

// Fork is one network upgrade.
type Fork struct {
	Name      string // e.g. "Glamsterdam", "BPO1"
	BPO       bool   // blob-parameter-only fork
	Execution string // execution layer fork name, e.g. "Amsterdam"
	Consensus string // consensus layer fork name, e.g. "Gloas"
	Epoch     uint64
	Time      time.Time // execution layer activation
	Postponed bool
	// MinVersions: releases the announcement lists for this fork.
	MinVersions map[ethereum.ClientName]releases.Version
	// For postponed forks: releases containing the former schedule and the
	// first releases that removed it.
	ScheduledIn map[ethereum.ClientName]releases.Version
	RemovedIn   map[ethereum.ClientName]releases.Version
	Sources     []string
}

// file mirrors forks.yaml; Parse converts it into a Schedule.
type file struct {
	Updated  string                 `yaml:"updated"`
	Networks map[string]networkFile `yaml:"networks"`
}

type networkFile struct {
	ChainID     uint64     `yaml:"chain_id"`
	GenesisTime int64      `yaml:"genesis_time"`
	Forks       []forkFile `yaml:"forks"`
}

type forkFile struct {
	Name        string            `yaml:"name"`
	Type        string            `yaml:"type"`
	Execution   string            `yaml:"execution"`
	Consensus   string            `yaml:"consensus"`
	Status      string            `yaml:"status"`
	Epoch       uint64            `yaml:"epoch"`
	Timestamp   int64             `yaml:"timestamp"`
	MinVersions map[string]string `yaml:"min_versions"`
	ScheduledIn map[string]string `yaml:"scheduled_in"`
	RemovedIn   map[string]string `yaml:"removed_in"`
	Sources     []string          `yaml:"sources"`
}

// Default returns the schedule compiled into x3f.
func Default() (*Schedule, error) {
	s, err := Parse(embedded)
	if err != nil {
		// Only possible with a broken build; tests guard against it.
		return nil, fmt.Errorf("embedded forks.yaml: %w", err)
	}
	return s, nil
}

// LoadFile reads a schedule from a file (the -forks flag).
func LoadFile(path string) (*Schedule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read forks file: %w", err)
	}
	s, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("forks file %s: %w", path, err)
	}
	return s, nil
}

// Parse parses and validates forks.yaml content. All problems are reported
// at once.
func Parse(data []byte) (*Schedule, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // typos in field names are errors
	var f file
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	var errs []error
	fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	s := &Schedule{Networks: map[string]Network{}}
	updated, err := time.Parse(time.DateOnly, f.Updated)
	if err != nil {
		fail("updated: want a date like 2026-11-24, got %q", f.Updated)
	}
	s.Updated = updated
	if len(f.Networks) == 0 {
		fail("no networks")
	}

	for name, nf := range f.Networks {
		known, ok := network.ByName(name)
		switch {
		case !ok:
			fail("network %q is not one of %s", name, strings.Join(network.Names(), ", "))
			continue
		case nf.ChainID != known.ChainID:
			fail("%s: chain_id %d, want %d", name, nf.ChainID, known.ChainID)
		case nf.GenesisTime <= 0:
			fail("%s: genesis_time missing", name)
		}
		n := Network{Name: name, ChainID: nf.ChainID, GenesisTime: nf.GenesisTime}

		for i, ff := range nf.Forks {
			label := fmt.Sprintf("%s: forks[%d] %q", name, i, ff.Name)
			fk := Fork{
				Name: ff.Name, Execution: ff.Execution, Consensus: ff.Consensus,
				Epoch: ff.Epoch, Time: time.Unix(ff.Timestamp, 0).UTC(),
				BPO: ff.Type == "bpo", Postponed: ff.Status == "postponed",
				Sources: ff.Sources,
			}
			if ff.Name == "" {
				fail("%s: name missing", label)
			}
			if ff.Type != "" && ff.Type != "bpo" {
				fail("%s: type %q, want \"bpo\" or nothing", label, ff.Type)
			}
			// Hard forks need both layer names: x3f matches them against
			// the <NAME>_FORK_EPOCH keys of the consensus client.
			if !fk.BPO && (ff.Execution == "" || ff.Consensus == "") {
				fail("%s: execution and consensus fork names are required", label)
			}
			if ff.Status != "" && ff.Status != "postponed" {
				fail("%s: status %q, want \"postponed\" or nothing", label, ff.Status)
			}
			// The timestamp must match the epoch. Both come from the
			// same announcement, so a mismatch is almost always a typo.
			if want := nf.GenesisTime + int64(ff.Epoch)*secondsPerEpoch; ff.Timestamp != want {
				fail("%s: timestamp %d does not match epoch %d (want %d)", label, ff.Timestamp, ff.Epoch, want)
			}
			if i > 0 && ff.Timestamp <= nf.Forks[i-1].Timestamp {
				fail("%s: forks must be in order of activation", label)
			}
			if len(ff.Sources) == 0 {
				fail("%s: at least one source is required", label)
			}
			for _, src := range ff.Sources {
				if !strings.HasPrefix(src, "https://") {
					fail("%s: source %q is not an https URL", label, src)
				}
			}
			if !fk.Postponed && (len(ff.ScheduledIn) > 0 || len(ff.RemovedIn) > 0) {
				fail("%s: scheduled_in/removed_in only apply to postponed forks", label)
			}
			fk.MinVersions = versions(ff.MinVersions, label+": min_versions", fail)
			fk.ScheduledIn = versions(ff.ScheduledIn, label+": scheduled_in", fail)
			fk.RemovedIn = versions(ff.RemovedIn, label+": removed_in", fail)
			n.Forks = append(n.Forks, fk)
		}
		s.Networks[name] = n
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return s, nil
}

// versions parses a client -> version map.
func versions(m map[string]string, label string, fail func(string, ...any)) map[ethereum.ClientName]releases.Version {
	out := make(map[ethereum.ClientName]releases.Version, len(m))
	for client, raw := range m {
		c := ethereum.ClientName(client)
		if _, ok := releases.RepoFor(c); !ok {
			fail("%s: unknown client %q", label, client)
			continue
		}
		v, ok := releases.ParseVersion(raw)
		if !ok {
			fail("%s: %s: invalid version %q", label, client, raw)
			continue
		}
		out[c] = v
	}
	return out
}

// ForChainID returns the network with the given chain ID.
func (s *Schedule) ForChainID(id uint64) (Network, bool) {
	for _, n := range s.Networks {
		if n.ChainID == id {
			return n, true
		}
	}
	return Network{}, false
}

// Active returns the forks that are active at now, oldest first.
func (n Network) Active(now time.Time) []Fork {
	var out []Fork
	for _, f := range n.Forks {
		if !f.Postponed && !f.Time.After(now) {
			out = append(out, f)
		}
	}
	return out
}

// Next returns the next scheduled fork after now.
func (n Network) Next(now time.Time) (Fork, bool) {
	for _, f := range n.Forks {
		if !f.Postponed && f.Time.After(now) {
			return f, true
		}
	}
	return Fork{}, false
}

// EpochTime returns the start time of a consensus layer epoch.
func (n Network) EpochTime(epoch uint64) time.Time {
	return time.Unix(n.GenesisTime+int64(epoch)*secondsPerEpoch, 0).UTC()
}

// Postponed returns the postponed forks.
func (n Network) Postponed() []Fork {
	var out []Fork
	for _, f := range n.Forks {
		if f.Postponed {
			out = append(out, f)
		}
	}
	return out
}
