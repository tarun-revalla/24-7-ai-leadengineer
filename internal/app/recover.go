package app

import (
	"context"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/recovery"
)

// Recover diagnoses whether the project was left mid-task by a crash or
// interruption. See internal/recovery for why this is read-only: the one
// ambiguous case — an interrupted task with uncommitted changes present — is
// left for a human to resolve with plain git commands rather than guessed at
// automatically.
func (a *App) Recover(ctx context.Context) (*recovery.Report, error) {
	analyzer := recovery.New(a.Git, a.Memory, a.Checkpoints)
	return analyzer.Analyze(ctx)
}
