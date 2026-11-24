// Command x3f reports whether Ethereum node clients are up to date,
// affected by known advisories and ready for the next hard fork.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/ropslabs/x3f/internal/config"
	"github.com/ropslabs/x3f/internal/ethereum"
)

func main() {
	// NotifyContext returns a context that is canceled on Ctrl+C, so
	// running requests stop cleanly instead of being killed mid-way.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// main only translates the result of run into an exit code. Keeping the
	// logic in run makes it testable and lets deferred calls run before exit
	// (os.Exit skips defers, hence the explicit stop()).
	if err := run(ctx, os.Args[1:], os.Stdout, ethereum.NewClient(nil)); err != nil {
		fmt.Fprintln(os.Stderr, "x3f:", err)
		stop()
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer, client *ethereum.Client) error {
	// A dedicated FlagSet instead of the global flag functions, so run can
	// be called with arbitrary arguments (e.g. from tests).
	fs := flag.NewFlagSet("x3f", flag.ContinueOnError)
	configPath := fs.String("config", "x3f.yaml", "path to the config file")
	timeout := fs.Duration("timeout", 10*time.Second, "timeout per request, e.g. 5s")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *timeout <= 0 {
		return errors.New("-timeout must be positive")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	results := checkNodes(ctx, client, cfg.Nodes, *timeout)
	failed, err := printResults(out, results)
	if err != nil {
		return err
	}
	if failed > 0 {
		// A non-zero exit code lets cron jobs and scripts notice failures.
		return fmt.Errorf("%d request(s) failed", failed)
	}
	return nil
}
