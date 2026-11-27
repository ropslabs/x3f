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
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ropslabs/x3f/internal/network"
)

// Config is the top-level structure of x3f.yaml.
type Config struct {
	// Telegram enables notifications; nil if not configured.
	Telegram *Telegram `yaml:"telegram,omitempty"`
	// IgnoreAdvisories applies to every node.
	IgnoreAdvisories []AdvisoryIgnore `yaml:"ignore_advisories,omitempty"`
	Nodes            []Node           `yaml:"nodes"`
}

// AdvisoryIgnore tells x3f not to count one security advisory, e.g. one
// about a feature the node does not use. Ignored advisories are still
// reported as a note, so nothing disappears silently.
type AdvisoryIgnore struct {
	// ID is the GitHub advisory ID, e.g. "GHSA-v5gq-r843-8f73". Validate
	// normalizes it to that spelling.
	ID string `yaml:"id"`
	// Reason is required: why the advisory does not apply.
	Reason string `yaml:"reason"`
	// Expires is optional. From this day on (00:00 UTC) the entry no
	// longer applies and the advisory counts again.
	Expires Date `yaml:"expires,omitempty"`
}

// Expired reports whether the entry has an expiry date that has been
// reached.
func (e AdvisoryIgnore) Expired(now time.Time) bool {
	return !e.Expires.IsZero() && !now.Before(e.Expires.Time)
}

// Date is a calendar day written as YYYY-MM-DD, stored as midnight UTC.
// Embedding time.Time gives Date all of its methods (IsZero, Format, ...).
type Date struct {
	time.Time
}

// UnmarshalYAML implements yaml.Unmarshaler: the YAML decoder calls it
// instead of decoding the value itself. It accepts only a plain date, not
// the full YAML timestamp syntax with a time of day.
func (d *Date) UnmarshalYAML(n *yaml.Node) error {
	t, err := time.Parse(time.DateOnly, n.Value)
	if n.Kind != yaml.ScalarNode || err != nil {
		return fmt.Errorf("line %d: %q is not a date (YYYY-MM-DD)", n.Line, n.Value)
	}
	d.Time = t
	return nil
}

// Telegram configures notifications through a Telegram bot. The bot token
// is deliberately not here: it comes from the environment variable
// X3F_TELEGRAM_BOT_TOKEN.
type Telegram struct {
	// ChatID is a numeric chat ID or "@channelname".
	ChatID string `yaml:"chat_id"`
	// MinLevel is "warn" (default) or "crit": the lowest status a node
	// change must reach to be reported. Leaving CRIT is always reported.
	// Validate normalizes it to lowercase and fills in the default.
	MinLevel string `yaml:"min_level,omitempty"`
	// RemindEvery is the reminder interval for a node that stays CRIT;
	// nil means DefaultRemindEvery, 0 disables reminders.
	RemindEvery *Duration `yaml:"remind_every,omitempty"`
}

// DefaultRemindEvery is the reminder interval if remind_every is not set.
const DefaultRemindEvery = 24 * time.Hour

// Remind returns the reminder interval.
func (t *Telegram) Remind() time.Duration {
	if t.RemindEvery == nil {
		return DefaultRemindEvery
	}
	return t.RemindEvery.Duration
}

// Duration is a duration written like "24h" or "90m" (Go syntax).
type Duration struct {
	time.Duration
}

// UnmarshalYAML implements yaml.Unmarshaler, like Date.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := time.ParseDuration(n.Value)
	if n.Kind != yaml.ScalarNode || err != nil || v < 0 {
		return fmt.Errorf("line %d: %q is not a duration such as 24h, 90m or 0", n.Line, n.Value)
	}
	d.Duration = v
	return nil
}

// chatIDRe matches a numeric chat ID (groups and channels are negative) or
// a public channel name.
var chatIDRe = regexp.MustCompile(`^(-?[0-9]+|@[A-Za-z0-9_]{5,32})$`)

func (t *Telegram) validate() []error {
	var errs []error
	t.ChatID = strings.TrimSpace(t.ChatID)
	if !chatIDRe.MatchString(t.ChatID) {
		// The value is not repeated: if someone pasted the bot token
		// here by mistake, it must not end up in the output.
		errs = append(errs, errors.New("telegram: chat_id must be a numeric chat ID or @channelname"))
	}
	switch m := strings.ToLower(strings.TrimSpace(t.MinLevel)); m {
	case "":
		t.MinLevel = "warn"
	case "warn", "crit":
		t.MinLevel = m
	default:
		errs = append(errs, fmt.Errorf("telegram: min_level %q is not warn or crit", t.MinLevel))
	}
	return errs
}

// MergeIgnores returns the ignore list for one node: its own entries plus
// the global ones. A node entry replaces a global entry with the same ID,
// e.g. to give it a different expiry date.
func MergeIgnores(global, node []AdvisoryIgnore) []AdvisoryIgnore {
	merged := slices.Clone(node)
	for _, g := range global {
		if !slices.ContainsFunc(node, func(n AdvisoryIgnore) bool { return n.ID == g.ID }) {
			merged = append(merged, g)
		}
	}
	return merged
}

// Node types. Each type is compiled into x3f.
const (
	// TypeEthereum is an Ethereum node: an execution client and a
	// consensus client. It is the type of a node without a type.
	TypeEthereum = "ethereum"
)

// Types lists the known node types.
var Types = []string{TypeEthereum}

// Node is one configured node. It is queried read-only. Which fields it
// needs depends on its type.
type Node struct {
	// Name identifies the node in output and alerts. It must be unique.
	Name string `yaml:"name"`
	// Type is the kind of node, one of Types; empty means TypeEthereum.
	// Validate normalizes it to lowercase and fills in the default.
	Type string `yaml:"type,omitempty"`
	// ExecutionURL is the JSON-RPC endpoint of the execution client.
	ExecutionURL string `yaml:"execution_url"`
	// BeaconURL is the beacon API endpoint of the consensus client.
	BeaconURL string `yaml:"beacon_url"`
	// Network is optional: if set ("mainnet", "sepolia", "hoodi", any
	// capitalisation), x3f reports it as critical when the node is on a
	// different network. Validate normalizes it to lowercase.
	Network string `yaml:"network,omitempty"`
	// IgnoreAdvisories applies to this node in addition to the global
	// list.
	IgnoreAdvisories []AdvisoryIgnore `yaml:"ignore_advisories,omitempty"`
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
	errs = append(errs, validateIgnores("ignore_advisories", c.IgnoreAdvisories)...)
	if c.Telegram != nil {
		errs = append(errs, c.Telegram.validate()...)
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

		typ := strings.ToLower(strings.TrimSpace(n.Type))
		if typ == "" {
			typ = TypeEthereum
		}
		if !slices.Contains(Types, typ) {
			errs = append(errs, fmt.Errorf("%s: type %q is not one of %s", label, n.Type, strings.Join(Types, ", ")))
		} else {
			c.Nodes[i].Type = typ
		}

		if typ == TypeEthereum {
			if err := validateURL(n.ExecutionURL); err != nil {
				errs = append(errs, fmt.Errorf("%s: execution_url: %w", label, err))
			}
			if err := validateURL(n.BeaconURL); err != nil {
				errs = append(errs, fmt.Errorf("%s: beacon_url: %w", label, err))
			}
		}
		if n.Network != "" {
			// Accept any capitalisation ("Mainnet") and store the
			// canonical lowercase name. n is a copy, so write back
			// through the slice index.
			normalized := strings.ToLower(strings.TrimSpace(n.Network))
			if _, ok := network.ByName(normalized); !ok {
				errs = append(errs, fmt.Errorf("%s: network %q is not one of %s",
					label, n.Network, strings.Join(network.Names(), ", ")))
			} else {
				c.Nodes[i].Network = normalized
			}
		}
		errs = append(errs, validateIgnores(label+": ignore_advisories", n.IgnoreAdvisories)...)
	}

	// errors.Join combines several errors into one; it returns nil if the
	// slice is empty.
	return errors.Join(errs...)
}

// ghsaRe matches a GitHub advisory ID after lowercasing, e.g.
// "ghsa-v5gq-r843-8f73".
var ghsaRe = regexp.MustCompile(`^ghsa(-[0-9a-z]{4}){3}$`)

// validateIgnores checks one ignore list and normalizes its IDs. The slice
// shares its backing array with the config, so writing list[i] changes the
// config itself.
func validateIgnores(label string, list []AdvisoryIgnore) []error {
	var errs []error
	seen := make(map[string]bool)
	for i, e := range list {
		item := fmt.Sprintf("%s[%d]", label, i)
		id := strings.ToLower(strings.TrimSpace(e.ID))
		if !ghsaRe.MatchString(id) {
			errs = append(errs, fmt.Errorf("%s: id %q is not a GitHub advisory ID such as GHSA-xxxx-xxxx-xxxx", item, e.ID))
		} else {
			id = "GHSA" + id[len("ghsa"):]
			list[i].ID = id
			item = fmt.Sprintf("%s %s", label, id)
			if seen[id] {
				errs = append(errs, fmt.Errorf("%s: listed twice", item))
			}
			seen[id] = true
		}
		if strings.TrimSpace(e.Reason) == "" {
			errs = append(errs, fmt.Errorf("%s: reason is required", item))
		}
	}
	return errs
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
