package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ropslabs/x3f/internal/status"
)

// stateVersion is the format of the state file; bump it on incompatible
// changes.
const stateVersion = 1

// State is what x3f remembers between runs, per node name.
type State struct {
	Version int                  `json:"version"`
	Nodes   map[string]NodeState `json:"nodes"`
}

// NodeState is the remembered state of one node.
type NodeState struct {
	// Level is the level seen on the last run, Since the time it was
	// first seen. Since says how long a problem has existed, even if the
	// message about it could only be delivered later.
	Level status.Level `json:"level"`
	Since time.Time    `json:"since"`
	// Told is the baseline for the next comparison: what the last
	// message said, or what changed without a message (below min_level).
	Told Told `json:"told"`
}

// Told is the status a node had in the last message about it. The zero
// value means "OK, never told", which is also what a new node starts from.
type Told struct {
	Level status.Level `json:"level"`
	// Checks holds the level of each check that was not OK.
	Checks map[string]status.Level `json:"checks,omitempty"`
	// At is when the last message about the node was sent.
	At time.Time `json:"at,omitzero"`
}

// LoadState reads the state file. A missing file is an empty state: the
// first run.
func LoadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{Version: stateVersion}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("state file %s: %w", path, err)
	}
	if s.Version != stateVersion {
		return State{}, fmt.Errorf("state file %s: version %d, want %d", path, s.Version, stateVersion)
	}
	return s, nil
}

// SaveState writes the state file atomically: into a temporary file in
// the same directory, then renamed over the old one. A crash or a full disk
// leaves the old file intact instead of a half-written one.
func SaveState(path string, s State) error {
	s.Version = stateVersion
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	// CreateTemp creates the file with mode 0600.
	f, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	_, err = f.Write(append(data, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}
