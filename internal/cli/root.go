package cli

import (
	"errors"
	"fmt"
	"time"

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
	var maxTasks int
	var once bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Run tasks from the backlog",
		Long: "Executes backlog tasks highest priority first.\n\n" +
			"Each task is implemented, verified against the quality gates, repaired\n" +
			"if they fail, and committed only once they pass. The working tree must\n" +
			"be clean before a task starts, so a commit cannot include unrelated work.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := flags.open()
			if err != nil {
				return err
			}
			defer a.Close()

			if once {
				maxTasks = 1
			}

			out := cmd.OutOrStdout()
			outcomes, runErr := a.RunLoop(cmd.Context(), maxTasks)

			for _, o := range outcomes {
				status := "no changes"
				if o.Committed {
					status = "committed " + shortHash(o.CommitHash)
				}
				fmt.Fprintf(out, "%-12s %s  (%s", o.Task.ID, o.Task.Title, status)
				if o.Repairs > 0 {
					fmt.Fprintf(out, ", %d repair(s)", o.Repairs)
				}
				fmt.Fprintf(out, ", %s)\n", o.Duration.Truncate(time.Millisecond))
			}

			if runErr != nil {
				// A partial run is still progress; report what completed
				// before surfacing why it stopped.
				if len(outcomes) > 0 {
					fmt.Fprintf(out, "\n%d task(s) completed before stopping.\n", len(outcomes))
				}
				return runErr
			}

			if len(outcomes) == 0 {
				fmt.Fprintln(out, "No open tasks in the backlog.")
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&maxTasks, "max-tasks", 0, "stop after this many tasks (0 for no limit)")
	cmd.Flags().BoolVar(&once, "once", false, "run a single task and stop")

	return cmd
}

func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
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
