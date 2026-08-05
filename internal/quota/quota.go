// Package quota tracks Claude usage limits and the cooldown they impose.
//
// A usage limit cannot be retried away, so hitting one is not an error to
// recover from but a schedule to obey: record when access should return,
// checkpoint, wait, resume. The waiting state is persisted, because a process
// that restarts mid-cooldown must keep waiting rather than resume straight
// into the same limit.
package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/atomicfile"
)

// StateFile is the name of the persisted quota state within the state dir.
const StateFile = "quota.json"

// DefaultCooldown is used when nothing better is known about when access
// returns. Usage windows are typically hourly.
const DefaultCooldown = time.Hour

// MaxCooldown caps exponential backoff so a run of misjudged windows cannot
// park the system for days.
const MaxCooldown = 6 * time.Hour

// State is the persisted view of quota availability.
type State struct {
	// Exhausted reports whether a usage limit is currently in force.
	Exhausted bool `json:"exhausted"`
	// DetectedAt is when the limit was last observed.
	DetectedAt time.Time `json:"detectedAt,omitempty"`
	// ResumeAt is when work may be attempted again.
	ResumeAt time.Time `json:"resumeAt,omitempty"`
	// ConsecutiveHits counts limits hit without a successful call between
	// them. Each one widens the next cooldown, since a limit met immediately
	// after resuming means the previous estimate was too optimistic.
	ConsecutiveHits int `json:"consecutiveHits"`
	// Reason records what was observed, for operators reading the file.
	Reason string `json:"reason,omitempty"`
	// LastCheckpoint links the cooldown to the state saved before it began.
	LastCheckpoint string `json:"lastCheckpoint,omitempty"`
}

// Clock supplies the current time.
type Clock func() time.Time

// Sleeper waits for d or until ctx is cancelled, returning ctx.Err() if
// cancelled first. Injected so cooldowns are testable without real delay.
type Sleeper func(ctx context.Context, d time.Duration) error

// Manager persists and enforces quota cooldowns.
type Manager struct {
	path    string
	clock   Clock
	sleep   Sleeper
	base    time.Duration
	maxWait time.Duration
	mu      sync.Mutex
}

// Option customises a Manager.
type Option func(*Manager)

// WithClock overrides the time source.
func WithClock(c Clock) Option {
	return func(m *Manager) { m.clock = c }
}

// WithSleeper overrides how waiting is performed.
func WithSleeper(s Sleeper) Option {
	return func(m *Manager) { m.sleep = s }
}

// WithCooldown sets the base cooldown applied when no reset time is known.
func WithCooldown(d time.Duration) Option {
	return func(m *Manager) {
		if d > 0 {
			m.base = d
		}
	}
}

// WithMaxCooldown caps the backoff.
func WithMaxCooldown(d time.Duration) Option {
	return func(m *Manager) {
		if d > 0 {
			m.maxWait = d
		}
	}
}

// New creates a quota manager storing its state under dir.
func New(dir string, opts ...Option) (*Manager, error) {
	if dir == "" {
		return nil, errors.New("quota state directory cannot be empty")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create quota state directory: %w", err)
	}

	m := &Manager{
		path:    filepath.Join(dir, StateFile),
		clock:   time.Now,
		sleep:   sleepWithContext,
		base:    DefaultCooldown,
		maxWait: MaxCooldown,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m, nil
}

// Current returns the persisted quota state.
//
// A missing or unreadable file yields available state rather than an error:
// refusing to run because the cooldown record is damaged would strand the
// system on exactly the file that exists to keep it running.
func (m *Manager) Current(ctx context.Context) *State {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.load()
}

// Available reports whether work may be attempted now, and if not, how long
// remains. A cooldown whose deadline has passed is available again.
func (m *Manager) Available(ctx context.Context) (bool, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.load()
	return m.availableAt(state, m.clock().UTC())
}

func (m *Manager) availableAt(state *State, now time.Time) (bool, time.Duration) {
	if !state.Exhausted {
		return true, 0
	}

	remaining := state.ResumeAt.Sub(now)
	if remaining <= 0 {
		return true, 0
	}

	return false, remaining
}

// RecordExhaustion marks the quota exhausted and schedules a resume time.
//
// resetHint is when the provider says access returns; a zero or past hint
// falls back to exponential backoff from the base cooldown. Repeated hits
// without an intervening success widen the wait, because meeting a limit
// straight after resuming means the previous estimate was too short.
func (m *Manager) RecordExhaustion(ctx context.Context, resetHint time.Time, reason string) (*State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.clock().UTC()
	previous := m.load()

	state := &State{
		Exhausted:       true,
		DetectedAt:      now,
		ConsecutiveHits: previous.ConsecutiveHits + 1,
		Reason:          reason,
		LastCheckpoint:  previous.LastCheckpoint,
	}

	state.ResumeAt = m.resumeTime(now, resetHint, state.ConsecutiveHits)

	if err := m.save(state); err != nil {
		return nil, err
	}

	return state, nil
}

// resumeTime prefers a provider-supplied reset time over an estimate.
func (m *Manager) resumeTime(now, hint time.Time, hits int) time.Time {
	if !hint.IsZero() && hint.After(now) {
		return hint.UTC()
	}

	wait := m.base
	for i := 1; i < hits; i++ {
		wait *= 2
		if wait >= m.maxWait {
			wait = m.maxWait
			break
		}
	}

	return now.Add(wait)
}

// NoteCheckpoint records which checkpoint captured the state that the current
// cooldown interrupted, so recovery knows where to resume from.
func (m *Manager) NoteCheckpoint(ctx context.Context, checkpointID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.load()
	state.LastCheckpoint = checkpointID

	return m.save(state)
}

// Clear marks quota available again and resets the backoff.
//
// It is called after a successful call, which is the only evidence that the
// limit has genuinely lifted.
func (m *Manager) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	previous := m.load()

	return m.save(&State{LastCheckpoint: previous.LastCheckpoint})
}

// WaitUntilAvailable blocks until the cooldown expires.
//
// It returns the time actually spent waiting. Cancellation is honoured
// immediately and returns the context error, leaving the persisted cooldown
// intact so a later run still observes it.
func (m *Manager) WaitUntilAvailable(ctx context.Context) (time.Duration, error) {
	m.mu.Lock()
	state := m.load()
	available, remaining := m.availableAt(state, m.clock().UTC())
	sleep := m.sleep
	m.mu.Unlock()

	if available {
		return 0, nil
	}

	if err := sleep(ctx, remaining); err != nil {
		return 0, err
	}

	return remaining, nil
}

// load reads persisted state. Callers must hold the lock.
func (m *Manager) load() *State {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return &State{}
	}

	state := &State{}
	if err := json.Unmarshal(data, state); err != nil {
		// A damaged file is treated as "no cooldown known". The alternative,
		// refusing to proceed, would turn a corrupt scratch file into an
		// outage that no automatic recovery could clear.
		return &State{}
	}

	return state
}

// save persists state. Callers must hold the lock.
func (m *Manager) save(state *State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode quota state: %w", err)
	}

	if err := atomicfile.Write(m.path, data, 0o644); err != nil {
		return fmt.Errorf("failed to persist quota state: %w", err)
	}

	return nil
}

// sleepWithContext waits for d, returning early if ctx is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
