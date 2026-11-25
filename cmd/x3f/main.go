// Command x3f reports whether Ethereum node clients are up to date,
// in sync, affected by known advisories and ready for the next hard fork.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/ropslabs/x3f/internal/config"
	"github.com/ropslabs/x3f/internal/ethereum"
	"github.com/ropslabs/x3f/internal/forks"
	"github.com/ropslabs/x3f/internal/releases"
	"github.com/ropslabs/x3f/internal/status"
)

// Exit codes follow the Nagios plugin convention, so monitoring systems can
// use x3f as a check directly.
const (
	exitOK      = 0 // everything green
	exitWarn    = 1 // at least one yellow
	exitCrit    = 2 // at least one red
	exitUnknown = 3 // x3f itself could not run (bad flags, bad config)
)

const usageText = `Usage: x3f [flags]

x3f queries the Ethereum nodes listed in the config file (read-only) and
reports the network, client versions, sync status, whether the clients are
behind their latest stable GitHub release and whether they are ready for
the next fork: the node's own fork schedule (eth_config,
/eth/v1/config/spec) is compared with the built-in one (see -forks); if a
client does not report it, its version is compared with the announced
releases.

Flags:
`

const helpFooter = `
Exit codes (Nagios plugin convention):
  0  OK       everything is green
  1  WARNING  at least one yellow finding
  2  CRITICAL at least one red finding
  3  UNKNOWN  x3f could not run (invalid flags or config)

Environment:
  GITHUB_TOKEN  optional GitHub token; raises the API rate limit from 60 to
                5000 requests per hour. Never stored or printed.
  NO_COLOR      disables colored output when set to a non-empty value.
`

// env bundles what run needs from the outside world, so tests can replace
// it: the HTTP client for nodes, the GitHub API location and token, the
// clock and whether to use colors.
type env struct {
	client        *ethereum.Client
	githubBaseURL string
	githubToken   string
	now           func() time.Time
	color         bool
}

func main() {
	// NotifyContext returns a context that is canceled on Ctrl+C, so
	// running requests stop cleanly instead of being killed mid-way.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, env{
		client:        ethereum.NewClient(nil),
		githubBaseURL: releases.DefaultBaseURL,
		githubToken:   os.Getenv("GITHUB_TOKEN"),
		now:           time.Now,
		color:         useColor(os.Stdout),
	})
	// os.Exit skips deferred calls, so stop is called explicitly.
	stop()
	os.Exit(code)
}

// useColor reports whether f is a terminal and NO_COLOR is not set.
// See https://no-color.org.
func useColor(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	info, err := f.Stat()
	// A character device is a terminal; files and pipes are not.
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// run executes x3f and returns the exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, e env) int {
	// A dedicated FlagSet instead of the global flag functions, so run can
	// be called with arbitrary arguments (e.g. from tests).
	fs := flag.NewFlagSet("x3f", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usageText)
		fs.PrintDefaults()
		fmt.Fprint(fs.Output(), helpFooter)
	}
	configPath := fs.String("config", "x3f.yaml", "path to the config file")
	timeout := fs.Duration("timeout", 10*time.Second, "timeout per request, e.g. 5s")
	graceDays := fs.Int("grace-days", 30, "days a client may lag behind the latest stable release before it turns red")
	cacheDir := fs.String("cache-dir", defaultCacheDir(), "directory for cached GitHub data (empty disables the cache)")
	cacheTTL := fs.Duration("cache-ttl", time.Hour, "how long cached GitHub data is used without asking GitHub")
	forksPath := fs.String("forks", "", "fork schedule file to use instead of the built-in one")
	forkCriticalDays := fs.Int("fork-critical-days", 14, "days before a fork from which a client that is not ready turns red (if a ready release exists)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK // -help was requested; usage is already printed
		}
		return exitUnknown // the flag package already printed the error
	}

	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "x3f: "+format+"\n", a...)
		return exitUnknown
	}
	if *timeout <= 0 {
		return fail("-timeout must be positive")
	}
	if *graceDays < 0 {
		return fail("-grace-days must not be negative")
	}
	if *cacheTTL < 0 {
		return fail("-cache-ttl must not be negative")
	}
	if *forkCriticalDays < 0 {
		return fail("-fork-critical-days must not be negative")
	}
	schedule, err := loadSchedule(*forksPath)
	if err != nil {
		return fail("%v", err)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fail("%v", err)
	}

	results := checkNodes(ctx, e.client, cfg.Nodes, *timeout)

	src := &releases.Source{
		BaseURL:  e.githubBaseURL,
		Token:    e.githubToken,
		CacheDir: *cacheDir,
		TTL:      *cacheTTL,
		Now:      e.now,
	}
	rel := fetchReleases(ctx, src, results, *timeout)
	refreshIfAhead(ctx, src, results, rel, *timeout)

	day := 24 * time.Hour
	now := e.now()
	rows, overall := assess(results, rel, assessParams{
		now:            now,
		grace:          time.Duration(*graceDays) * day,
		schedule:       schedule,
		forkCriticalIn: time.Duration(*forkCriticalDays) * day,
	})
	var global []status.Finding
	if f, ok := status.ForkDataNote(schedule, now); ok {
		global = append(global, f)
	}
	if err := printReport(stdout, rows, global, overall, e.color); err != nil {
		return fail("%v", err)
	}

	switch overall {
	case status.OK:
		return exitOK
	case status.Warn:
		return exitWarn
	default:
		return exitCrit
	}
}

// loadSchedule returns the fork schedule from path, or the built-in one if
// path is empty.
func loadSchedule(path string) (*forks.Schedule, error) {
	if path == "" {
		return forks.Default()
	}
	return forks.LoadFile(path)
}

// defaultCacheDir returns the per-user cache directory, e.g. ~/.cache/x3f
// on Linux (respecting XDG_CACHE_HOME). Empty if it cannot be determined.
func defaultCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "x3f")
}
