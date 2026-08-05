package cli

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newInitCommand(flags *globalFlags) *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the project state directory",
		Long: "Creates .ai with the memory documents the system reads and writes.\n\n" +
			"Safe to re-run: existing documents are left untouched.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := flags.open()
			if err != nil {
				return err
			}
			defer func() { _ = a.Close() }()

			result, err := a.Initialize(cmd.Context(), name)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Initialised %s\n", a.StatePath())

			for _, f := range result.Created {
				_, _ = fmt.Fprintf(out, "  created  %s\n", f)
			}
			for _, f := range result.Skipped {
				_, _ = fmt.Fprintf(out, "  kept     %s\n", f)
			}

			if len(result.Created) == 0 {
				_, _ = fmt.Fprintln(out, "\nAlready initialised; nothing changed.")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "project name (defaults to the directory name)")
	return cmd
}

func newStatusCommand(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current project and system state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := flags.open()
			if err != nil {
				return err
			}
			defer func() { _ = a.Close() }()

			status := a.Status(cmd.Context())
			_, _ = fmt.Fprint(cmd.OutOrStdout(), status.Render())
			return nil
		},
	}
}

func newCheckpointCommand(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkpoint",
		Short: "Inspect and manage saved state",
	}

	cmd.AddCommand(
		newCheckpointListCommand(flags),
		newCheckpointShowCommand(flags),
		newCheckpointVerifyCommand(flags),
		newCheckpointPruneCommand(flags),
	)

	return cmd
}

func newCheckpointListCommand(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved checkpoints, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := flags.open()
			if err != nil {
				return err
			}
			defer func() { _ = a.Close() }()

			list, err := a.Checkpoints.ListCheckpoints(cmd.Context())
			if err != nil {
				return err
			}
			if len(list) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No checkpoints recorded.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tTAKEN\tTASK\tSIZE\tSTATE")

			for _, md := range list {
				state := "ok"
				if err := a.Checkpoints.ValidateCheckpoint(cmd.Context(), md.ID); err != nil {
					state = "CORRUPT"
				}

				task := md.TaskID
				if task == "" {
					task = "-"
				}

				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d B\t%s\n",
					md.ID, md.Timestamp.Format("2006-01-02 15:04:05"), task, md.Size, state)
			}

			return w.Flush()
		},
	}
}

func newCheckpointShowCommand(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "show <checkpoint-id>",
		Short: "Show the contents of a checkpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := flags.open()
			if err != nil {
				return err
			}
			defer func() { _ = a.Close() }()

			state, err := a.Checkpoints.LoadCheckpoint(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "ID:        %s\n", state.ID)
			_, _ = fmt.Fprintf(out, "Taken:     %s\n", state.Timestamp.Format("2006-01-02 15:04:05 MST"))
			_, _ = fmt.Fprintf(out, "Task:      %s\n", orDash(state.TaskID))
			_, _ = fmt.Fprintf(out, "State:     %s\n", orDash(state.TaskState))
			_, _ = fmt.Fprintf(out, "Progress:  %.0f%%\n", state.Progress*100)
			_, _ = fmt.Fprintf(out, "Commit:    %s\n", orDash(state.GitCommit))

			if q := state.QuotaState; q != nil {
				_, _ = fmt.Fprintf(out, "Quota:     %.0f%% remaining\n", q.Remaining*100)
				if !q.ResetTime.IsZero() {
					_, _ = fmt.Fprintf(out, "Resets:    %s\n", q.ResetTime.Format("2006-01-02 15:04:05 MST"))
				}
			}

			return nil
		},
	}
}

func newCheckpointVerifyCommand(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Check every checkpoint's integrity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := flags.open()
			if err != nil {
				return err
			}
			defer func() { _ = a.Close() }()

			list, err := a.Checkpoints.ListCheckpoints(cmd.Context())
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			var corrupt int

			for _, md := range list {
				if err := a.Checkpoints.ValidateCheckpoint(cmd.Context(), md.ID); err != nil {
					corrupt++
					_, _ = fmt.Fprintf(out, "CORRUPT  %s: %v\n", md.ID, err)
					continue
				}
				_, _ = fmt.Fprintf(out, "ok       %s\n", md.ID)
			}

			_, _ = fmt.Fprintf(out, "\n%d checkpoint(s), %d corrupt\n", len(list), corrupt)

			if corrupt > 0 {
				// Recovery skips corrupt checkpoints, so this is not fatal —
				// but it must not be reported as a clean result either.
				return fmt.Errorf("%d checkpoint(s) failed verification", corrupt)
			}
			return nil
		},
	}
}

func newCheckpointPruneCommand(flags *globalFlags) *cobra.Command {
	var keep int

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete old checkpoints, keeping the most recent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := flags.open()
			if err != nil {
				return err
			}
			defer func() { _ = a.Close() }()

			if keep <= 0 {
				keep = a.Config.GetInt("checkpoint.retention")
			}

			before, err := a.Checkpoints.ListCheckpoints(cmd.Context())
			if err != nil {
				return err
			}

			if err := a.Checkpoints.PruneOldCheckpoints(cmd.Context(), keep); err != nil {
				return err
			}

			after, err := a.Checkpoints.ListCheckpoints(cmd.Context())
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed %d checkpoint(s); %d retained.\n",
				len(before)-len(after), len(after))
			return nil
		},
	}

	cmd.Flags().IntVar(&keep, "keep", 0, "number to retain (defaults to checkpoint.retention)")
	return cmd
}

func newConfigCommand(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect configuration",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show the effective configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := flags.open()
			if err != nil {
				return err
			}
			defer func() { _ = a.Close() }()

			spec := a.Config.GetSpec()
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

			_, _ = fmt.Fprintf(w, "project.name\t%s\n", spec.Project.Name)
			_, _ = fmt.Fprintf(w, "project.type\t%s\n", spec.Project.Type)
			_, _ = fmt.Fprintf(w, "claude.enabled\t%t\n", spec.Claude.Enabled)
			_, _ = fmt.Fprintf(w, "claude.model\t%s\n", spec.Claude.Model)
			_, _ = fmt.Fprintf(w, "claude.maxRetries\t%d\n", spec.Claude.MaxRetries)
			_, _ = fmt.Fprintf(w, "claude.timeoutSeconds\t%d\n", spec.Claude.TimeoutSeconds)
			_, _ = fmt.Fprintf(w, "quota.warningThreshold\t%.2f\n", spec.Quota.WarningThreshold)
			_, _ = fmt.Fprintf(w, "quota.exhaustionThreshold\t%.2f\n", spec.Quota.ExhaustionThreshold)
			_, _ = fmt.Fprintf(w, "quota.autoSleepOnExhaustion\t%t\n", spec.Quota.AutoSleepOnExhaustion)
			_, _ = fmt.Fprintf(w, "checkpoint.retention\t%d\n", spec.Checkpoint.Retention)
			_, _ = fmt.Fprintf(w, "checkpoint.compression\t%t\n", spec.Checkpoint.Compression)
			_, _ = fmt.Fprintf(w, "git.committerName\t%s\n", spec.Git.CommitterName)
			_, _ = fmt.Fprintf(w, "git.committerEmail\t%s\n", spec.Git.CommitterEmail)
			_, _ = fmt.Fprintf(w, "logging.level\t%s\n", spec.Logging.Level)
			_, _ = fmt.Fprintf(w, "logging.format\t%s\n", spec.Logging.Format)

			return w.Flush()
		},
	})

	return cmd
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func newQuotaCommand(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quota",
		Short: "Inspect and manage the Claude usage cooldown",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show whether Claude may be called now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := flags.open()
			if err != nil {
				return err
			}
			defer func() { _ = a.Close() }()

			ctx := cmd.Context()
			state := a.Quota.Current(ctx)
			available, remaining := a.Quota.Available(ctx)

			out := cmd.OutOrStdout()
			if available {
				_, _ = fmt.Fprintln(out, "Quota available.")
				if state.ConsecutiveHits > 0 {
					_, _ = fmt.Fprintf(out, "Last limit hit %s.\n", state.DetectedAt.Format(time.RFC3339))
				}
				return nil
			}

			_, _ = fmt.Fprintf(out, "In cooldown for another %s.\n", remaining.Truncate(time.Second))
			_, _ = fmt.Fprintf(out, "Resumes:    %s\n", state.ResumeAt.Format(time.RFC3339))
			_, _ = fmt.Fprintf(out, "Detected:   %s\n", state.DetectedAt.Format(time.RFC3339))
			_, _ = fmt.Fprintf(out, "Reason:     %s\n", orDash(state.Reason))
			_, _ = fmt.Fprintf(out, "Hits:       %d consecutive\n", state.ConsecutiveHits)
			_, _ = fmt.Fprintf(out, "Checkpoint: %s\n", orDash(state.LastCheckpoint))
			return nil
		},
	})

	clear := &cobra.Command{
		Use:   "clear",
		Short: "End the cooldown early",
		Long: "Clears the recorded cooldown so work resumes immediately.\n\n" +
			"Cooldowns are estimated when the provider gives no reset time, so one\n" +
			"can outlast the limit it was protecting against. Clearing a cooldown\n" +
			"that is still in force will simply hit the limit again.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := flags.open()
			if err != nil {
				return err
			}
			defer func() { _ = a.Close() }()

			ctx := cmd.Context()
			if available, _ := a.Quota.Available(ctx); available {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No cooldown in force; nothing to clear.")
				return nil
			}

			if err := a.Quota.Clear(ctx); err != nil {
				return err
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Cooldown cleared; work may resume.")
			return nil
		},
	}
	cmd.AddCommand(clear)

	return cmd
}
