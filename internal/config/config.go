// Package config loads and validates the x3f configuration file (x3f.yaml).
//
// The config file only describes what to inspect. Secrets such as API tokens
// never live here; they come from environment variables.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Config is the top-level structure of x3f.yaml.
type Config struct {
	Nodes []Node `yaml:"nodes"`
}

// Node is one Ethereum node, made up of an execution client and a consensus
// client. Both are queried read-only.
type Node struct {
	// Name identifies the node in output and alerts. It must be unique.
	Name string `yaml:"name"`
	// ExecutionURL is the JSON-RPC endpoint of the execution client.
	ExecutionURL string `yaml:"execution_url"`
	// BeaconURL is the beacon API endpoint of the consensus client.
	BeaconURL string `yaml:"beacon_url"`
}

// Load reads the config file at path, parses it and validates it.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// %w wraps the original error so callers can still inspect it,
		// e.g. errors.Is(err, fs.ErrNotExist).
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// Parse parses and validates config data. It is separate from Load so that
// tests and other callers can work without touching the filesystem.
func Parse(data []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	// Reject keys that do not exist in the struct, so a typo such as
	// "beacon_ulr" fails loudly instead of being silently ignored.
	dec.KnownFields(true)

	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		// io.EOF means the document was empty.
		if errors.Is(err, io.EOF) {
			return nil, errors.New("config is empty")
		}
		return nil, fmt.Errorf("parse: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks the config for mistakes. It reports all problems at once
// rather than stopping at the first one, so users can fix them in one go.
func (c *Config) Validate() error {
	var errs []error

	if len(c.Nodes) == 0 {
		errs = append(errs, errors.New("no nodes configured"))
	}

	// map[string]bool used as a set to detect duplicate names.
	seen := make(map[string]bool)
	for i, n := range c.Nodes {
		label := fmt.Sprintf("nodes[%d]", i)
		name := strings.TrimSpace(n.Name)
		if name == "" {
			errs = append(errs, fmt.Errorf("%s: name is required", label))
		} else {
			label = fmt.Sprintf("node %q", name)
			if seen[name] {
				errs = append(errs, fmt.Errorf("%s: duplicate name", label))
			}
			seen[name] = true
		}

		if err := validateURL(n.ExecutionURL); err != nil {
			errs = append(errs, fmt.Errorf("%s: execution_url: %w", label, err))
		}
		if err := validateURL(n.BeaconURL); err != nil {
			errs = append(errs, fmt.Errorf("%s: beacon_url: %w", label, err))
		}
	}

	// errors.Join combines several errors into one; it returns nil if the
	// slice is empty.
	return errors.Join(errs...)
}

func validateURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		// The *url.Error message contains the full URL, which may include an
		// API key. Report only the underlying reason.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("invalid URL: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("host is missing")
	}
	return nil
}
