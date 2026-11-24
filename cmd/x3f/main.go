// Command x3f reports whether Ethereum node clients are up to date,
// affected by known advisories and ready for the next hard fork.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ropslabs/x3f/internal/config"
)

func main() {
	// main only translates the result of run into an exit code. Keeping the
	// logic in run makes it testable and lets deferred calls run before exit
	// (os.Exit skips defers).
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "x3f:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	// A dedicated FlagSet instead of the global flag functions, so run can
	// be called with arbitrary arguments (e.g. from tests).
	fs := flag.NewFlagSet("x3f", flag.ContinueOnError)
	configPath := fs.String("config", "x3f.yaml", "path to the config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	// Placeholder output until version collection exists (build step 2).
	fmt.Fprintf(out, "Loaded %d node(s) from %s:\n", len(cfg.Nodes), *configPath)
	for _, n := range cfg.Nodes {
		fmt.Fprintf(out, "  %s  execution=%s  beacon=%s\n", n.Name, n.ExecutionURL, n.BeaconURL)
	}
	return nil
}
