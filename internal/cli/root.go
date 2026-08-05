package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/app"
)

// globalFlags are shared by every command.
type globalFlags struct {
	projectPath string
	configFile  string
}

// ErrNotImplemented marks a command that is declared but not yet built.
//
// Returning it is deliberate: a stub that returns nil reports success for work
// it never did, which is exactly the silent failure this system must not have.
var ErrNotImplemented = errors.New("not implemented yet")

// NewRootCommand creates the root CLI command.
func NewRootCommand(version, commit, date string) *cobra.Command {
	flags := &globalFlags{}

	cmd := &cobra.Command{
		Use:   "leadengineer",
		Short: "Autonomous AI software engineering platform",
		Long: "An autonomous engineering system that uses Claude Code as its execution engine.\n\n" +
			"State lives in the project's .ai directory and survives restarts.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.SetVersionTemplate(fmt.Sprintf("leadengineer %s (commit %s, built %s)\n", version, commit, date))

	pf := cmd.PersistentFlags()
	pf.StringVarP(&flags.projectPath, "project", "C", ".", "path to the project repository")
	pf.StringVar(&flags.configFile, "config", "", "path to a configuration file")

	cmd.AddCommand(
		newInitCommand(flags),
		newStatusCommand(flags),
		newCheckpointCommand(flags),
		newConfigCommand(flags),
		newQuotaCommand(flags),
		newStartCommand(flags),
		newRecoverCommand(flags),
	)

	return cmd
}

// open builds the application graph for a command invocation.
func (f *globalFlags) open() (*app.App, error) {
	return app.New(app.Options{
		ProjectPath: f.projectPath,
		ConfigFile:  f.configFile,
		LogToStderr: true,
	})
}

func newStartCommand(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Run the autonomous execution loop",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("start: %w — the task executor is not built; "+
				"`status` and `checkpoint` operate on real state today", ErrNotImplemented)
		},
	}
}

func newRecoverCommand(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "recover",
		Short: "Resume work interrupted by a crash or usage limit",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("recover: %w — inspect state with `status` and "+
				"`checkpoint list` in the meantime", ErrNotImplemented)
		},
	}
}
