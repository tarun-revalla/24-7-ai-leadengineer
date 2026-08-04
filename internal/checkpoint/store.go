package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

const (
	fileExt = ".ckpt.json"
	// idTimeLayout sorts lexicographically in chronological order, so
	// filename ordering and time ordering never disagree.
	idTimeLayout = "20060102T150405.000000000Z"
)

// ErrNoCheckpoints indicates the store holds no usable checkpoint.
var ErrNoCheckpoints = errors.New("no checkpoints available")

// Store must satisfy the contract the rest of the system depends on.
var _ interfaces.CheckpointManager = (*Store)(nil)

// Clock supplies the current time. Injected so ordering and retention
// behaviour can be tested without sleeping.
type Clock func() time.Time

// Store persists checkpoints as verifiable files on disk.
//
// Writes are atomic: content is written to a temporary file, flushed, then
// renamed into place. A crash therefore leaves either the previous checkpoint
// or the new one, never a partially written file.
type Store struct {
	dir      string
	compress bool
	clock    Clock
	seq      atomic.Uint64
	mu       sync.RWMutex
}

// Option customises a Store.
type Option func(*Store)

// WithCompression enables gzip compression of checkpoint payloads.
func WithCompression(on bool) Option {
	return func(s *Store) { s.compress = on }
}

// WithClock overrides the time source.
func WithClock(c Clock) Option {
	return func(s *Store) { s.clock = c }
}

// NewStore creates a checkpoint store rooted at dir, creating it if needed.
func NewStore(dir string, opts ...Option) (*Store, error) {
	if dir == "" {
		return nil, errors.New("checkpoint directory cannot be empty")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create checkpoint directory: %w", err)
	}

	s := &Store{
		dir:   dir,
		clock: time.Now,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s, nil
}

// CreateCheckpoint writes state to disk and returns its identifier.
func (s *Store) CreateCheckpoint(ctx context.Context, state *interfaces.CheckpointState) (string, error) {
	if state == nil {
		return "", errors.New("checkpoint state cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock().UTC()
	if state.Timestamp.IsZero() {
		state.Timestamp = now
	}

	id := s.nextID(now)
	state.ID = id

	data, err := encode(state, s.compress)
	if err != nil {
		return "", err
	}

	if err := s.writeAtomic(s.pathFor(id), data); err != nil {
		return "", err
	}

	return id, nil
}

// LoadCheckpoint reads and verifies the checkpoint with the given id.
func (s *Store) LoadCheckpoint(ctx context.Context, id string) (*interfaces.CheckpointState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.load(id)
}

// ListCheckpoints returns metadata for every checkpoint file, newest first.
// Corrupt files are included so that operators can see them; use
// ValidateCheckpoint or GetLatestCheckpoint to distinguish usable ones.
func (s *Store) ListCheckpoints(ctx context.Context) ([]interfaces.CheckpointMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids, err := s.listIDs()
	if err != nil {
		return nil, err
	}

	out := make([]interfaces.CheckpointMetadata, 0, len(ids))
	for _, id := range ids {
		md := interfaces.CheckpointMetadata{ID: id}

		if info, err := os.Stat(s.pathFor(id)); err == nil {
			md.Size = info.Size()
		}
		if ts, err := time.Parse(idTimeLayout, timePartOf(id)); err == nil {
			md.Timestamp = ts
		}
		// TaskID requires reading the payload; a corrupt file simply leaves
		// it blank rather than failing the whole listing.
		if state, err := s.load(id); err == nil {
			md.TaskID = state.TaskID
			md.Timestamp = state.Timestamp
		}

		out = append(out, md)
	}

	return out, nil
}

// GetLatestCheckpoint returns the most recent checkpoint that passes
// verification. Corrupt checkpoints are skipped rather than returned or
// treated as fatal: a torn or damaged newest file must not block recovery
// when an older intact one exists.
func (s *Store) GetLatestCheckpoint(ctx context.Context) (*interfaces.CheckpointState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids, err := s.listIDs()
	if err != nil {
		return nil, err
	}

	for _, id := range ids {
		state, err := s.load(id)
		if err == nil {
			return state, nil
		}
		if !errors.Is(err, ErrCorrupt) {
			return nil, err
		}
	}

	return nil, ErrNoCheckpoints
}

// ValidateCheckpoint verifies a checkpoint without returning its contents.
func (s *Store) ValidateCheckpoint(ctx context.Context, id string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, err := s.load(id)
	return err
}

// DeleteCheckpoint removes a checkpoint.
func (s *Store) DeleteCheckpoint(ctx context.Context, id string) error {
	if err := validateID(id); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.pathFor(id)); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("checkpoint %s not found", id)
		}
		return fmt.Errorf("failed to delete checkpoint %s: %w", id, err)
	}

	return nil
}

// PruneOldCheckpoints retains the keep most recent checkpoints and deletes the
// rest. Corrupt checkpoints are pruned first, before any intact one is
// discarded, so retention never trades a usable checkpoint for a damaged one.
func (s *Store) PruneOldCheckpoints(ctx context.Context, keep int) error {
	if keep < 1 {
		return errors.New("retention count must be at least 1")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ids, err := s.listIDs()
	if err != nil {
		return err
	}
	if len(ids) <= keep {
		return nil
	}

	var intact, corrupt []string
	for _, id := range ids {
		if _, err := s.load(id); err != nil && errors.Is(err, ErrCorrupt) {
			corrupt = append(corrupt, id)
			continue
		}
		intact = append(intact, id)
	}

	// Drop every corrupt file, then trim the oldest intact ones down to keep.
	doomed := corrupt
	if len(intact) > keep {
		doomed = append(doomed, intact[keep:]...)
	}

	for _, id := range doomed {
		if err := os.Remove(s.pathFor(id)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to prune checkpoint %s: %w", id, err)
		}
	}

	return nil
}

// load reads one checkpoint. Callers must hold at least a read lock.
func (s *Store) load(id string) (*interfaces.CheckpointState, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(s.pathFor(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("checkpoint %s not found", id)
		}
		return nil, fmt.Errorf("failed to read checkpoint %s: %w", id, err)
	}

	state := &interfaces.CheckpointState{}
	if err := decode(data, state); err != nil {
		return nil, fmt.Errorf("checkpoint %s: %w", id, err)
	}

	return state, nil
}

// listIDs returns checkpoint ids sorted newest first.
func (s *Store) listIDs() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read checkpoint directory: %w", err)
	}

	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), fileExt) {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), fileExt))
	}

	// Ids begin with a lexicographically sortable timestamp, so a plain
	// reverse sort yields newest first.
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))

	return ids, nil
}

// writeAtomic writes data so that readers observe either the old file or the
// complete new one. The temporary file is created in the destination
// directory so the rename stays within one filesystem.
func (s *Store) writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".tmp-checkpoint-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary checkpoint file: %w", err)
	}
	tmpName := tmp.Name()

	// Remove the temporary file on any path that does not reach the rename.
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write checkpoint: %w", err)
	}

	// Flush to storage before the rename, otherwise a power loss can leave the
	// directory entry pointing at unwritten data.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to flush checkpoint: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close checkpoint: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("failed to commit checkpoint: %w", err)
	}
	tmpName = ""

	return syncDir(dir)
}

// syncDir flushes a directory entry so a rename survives power loss.
// Not all platforms permit opening a directory for sync; failure to do so is
// not treated as a checkpoint failure since the data itself is already durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer d.Close()

	_ = d.Sync()
	return nil
}

// nextID builds an identifier that sorts chronologically. The counter
// disambiguates checkpoints created within the same clock tick, which a
// coarse or frozen clock makes likely.
func (s *Store) nextID(now time.Time) string {
	return fmt.Sprintf("ckpt-%s-%06d", now.UTC().Format(idTimeLayout), s.seq.Add(1))
}

func timePartOf(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}

func (s *Store) pathFor(id string) string {
	return filepath.Join(s.dir, id+fileExt)
}

// validateID rejects identifiers that could escape the checkpoint directory.
func validateID(id string) error {
	if id == "" {
		return errors.New("checkpoint id cannot be empty")
	}
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return fmt.Errorf("invalid checkpoint id: %q", id)
	}
	return nil
}
