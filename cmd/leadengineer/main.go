// Command leadengineer runs the autonomous engineering system.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/cli"
)

// Populated at build time via -ldflags.
var (
	version = "0.1.0-alpha"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	// Cancel on interrupt so in-flight subprocesses are terminated and state
	// is left consistent rather than abandoned mid-write.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := cli.NewRootCommand(version, commit, date)

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)

		if errors.Is(err, context.Canceled) {
			// Conventional exit status for termination by SIGINT.
			os.Exit(130)
		}
		os.Exit(1)
	}
}
