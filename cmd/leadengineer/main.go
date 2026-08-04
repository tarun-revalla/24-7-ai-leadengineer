package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/cli"
)

var (
	version = "0.1.0-alpha"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	rootCmd := cli.NewRootCommand(version, commit, date)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
