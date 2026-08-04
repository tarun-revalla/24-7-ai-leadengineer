package checkpoint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

func newStore(t *testing.T, opts ...Option) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir(), opts...)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	return s
}

func sampleState(taskID string) *interfaces.CheckpointState {
	return &interfaces.CheckpointState{
		TaskID:    taskID,
		TaskState: "implementing",
		Progress:  0.5,
		GitCommit: "abc123",
		QuotaState: &interfaces.QuotaSnapshot{
			Remaining: 0.75,
			ResetTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		Metrics: map[string]interface{}{"attempts": float64(2)},
	}
}

func TestNewStoreCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "checkpoints")

	if _, err := NewStore(dir); err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("directory not created: %v", err)
	}
}

func TestNewStoreRejectsEmptyDir(t *testing.T) {
	if _, err := NewStore(""); err == nil {
		t.Fatal("empty directory must be rejected")
	}
}

func TestCreateAndLoadRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	id, err := s.CreateCheckpoint(ctx, sampleState("task-1"))
	if err != nil {
		t.Fatalf("CreateCheckpoint failed: %v", err)
	}
	if id == "" {
		t.Fatal("returned id is empty")
	}

	got, err := s.LoadCheckpoint(ctx, id)
	if err != nil {
		t.Fatalf("LoadCheckpoint failed: %v", err)
	}

	if got.TaskID != "task-1" {
		t.Errorf("TaskID: got %q, want task-1", got.TaskID)
	}
	if got.Progress != 0.5 {
		t.Errorf("Progress: got %v, want 0.5", got.Progress)
	}
	if got.ID != id {
		t.Errorf("ID: got %q, want %q", got.ID, id)
	}
	if got.QuotaState == nil || got.QuotaState.Remaining != 0.75 {
		t.Errorf("QuotaState not preserved: %+v", got.QuotaState)
	}
	if got.Timestamp.IsZero() {
		t.Error("Timestamp should be populated")
	}
}

func TestCreateRejectsNilState(t *testing.T) {
	s := newStore(t)
	if _, err := s.CreateCheckpoint(context.Background(), nil); err == nil {
		t.Fatal("nil state must be rejected")
	}
}

func TestCompressedRoundTrip(t *testing.T) {
	s := newStore(t, WithCompression(true))
	ctx := context.Background()

	id, err := s.CreateCheckpoint(ctx, sampleState("task-compressed"))
	if err != nil {
		t.Fatalf("CreateCheckpoint failed: %v", err)
	}

	got, err := s.LoadCheckpoint(ctx, id)
	if err != nil {
		t.Fatalf("LoadCheckpoint failed: %v", err)
	}
	if got.TaskID != "task-compressed" {
		t.Errorf("TaskID: got %q, want task-compressed", got.TaskID)
	}
}

func TestCorruptionIsDetected(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	id, _ := s.CreateCheckpoint(ctx, sampleState("task-1"))
	path := s.pathFor(id)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	// Flip a byte inside the payload, leaving the envelope structurally valid.
	corrupted := strings.Replace(string(raw), `"task-1"`, `"task-X"`, 1)
	if corrupted == string(raw) {
		t.Fatal("test setup did not modify the payload")
	}
	if err := os.WriteFile(path, []byte(corrupted), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, err = s.LoadCheckpoint(ctx, id)
	if err == nil {
		t.Fatal("tampered payload must not load successfully")
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Errorf("expected ErrCorrupt, got %v", err)
	}
}

func TestTruncatedFileIsCorrupt(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	id, _ := s.CreateCheckpoint(ctx, sampleState("task-1"))

	raw, _ := os.ReadFile(s.pathFor(id))
	if err := os.WriteFile(s.pathFor(id), raw[:len(raw)/2], 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := s.ValidateCheckpoint(ctx, id); !errors.Is(err, ErrCorrupt) {
		t.Errorf("expected ErrCorrupt for truncated file, got %v", err)
	}
}

func TestCorruptCompressedBlobIsCorrupt(t *testing.T) {
	s := newStore(t, WithCompression(true))
	ctx := context.Background()

	id, _ := s.CreateCheckpoint(ctx, sampleState("task-1"))

	raw, _ := os.ReadFile(s.pathFor(id))
	// Replace the base64 blob with something that is valid base64 but not gzip.
	broken := strings.Replace(string(raw), `"blob":"`, `"blob":"AAAA`, 1)
	if err := os.WriteFile(s.pathFor(id), []byte(broken), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := s.ValidateCheckpoint(ctx, id); !errors.Is(err, ErrCorrupt) {
		t.Errorf("expected ErrCorrupt for damaged gzip blob, got %v", err)
	}
}

// The central recovery guarantee: a damaged newest checkpoint must not strand
// the system when an older intact one is present.
func TestGetLatestSkipsCorruptCheckpoint(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	oldID, _ := s.CreateCheckpoint(ctx, sampleState("older-good"))
	newID, _ := s.CreateCheckpoint(ctx, sampleState("newer-bad"))

	if err := os.WriteFile(s.pathFor(newID), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	got, err := s.GetLatestCheckpoint(ctx)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint failed: %v", err)
	}

	if got.ID != oldID {
		t.Errorf("expected fallback to %s, got %s", oldID, got.ID)
	}
	if got.TaskID != "older-good" {
		t.Errorf("TaskID: got %q, want older-good", got.TaskID)
	}
}

func TestGetLatestReturnsNewest(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for _, name := range []string{"first", "second", "third"} {
		if _, err := s.CreateCheckpoint(ctx, sampleState(name)); err != nil {
			t.Fatalf("CreateCheckpoint failed: %v", err)
		}
	}

	got, err := s.GetLatestCheckpoint(ctx)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint failed: %v", err)
	}
	if got.TaskID != "third" {
		t.Errorf("TaskID: got %q, want third", got.TaskID)
	}
}

func TestGetLatestEmptyStore(t *testing.T) {
	s := newStore(t)

	_, err := s.GetLatestCheckpoint(context.Background())
	if !errors.Is(err, ErrNoCheckpoints) {
		t.Errorf("expected ErrNoCheckpoints, got %v", err)
	}
}

func TestGetLatestAllCorrupt(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		id, _ := s.CreateCheckpoint(ctx, sampleState("x"))
		os.WriteFile(s.pathFor(id), []byte("garbage"), 0o644)
	}

	_, err := s.GetLatestCheckpoint(ctx)
	if !errors.Is(err, ErrNoCheckpoints) {
		t.Errorf("expected ErrNoCheckpoints when nothing is usable, got %v", err)
	}
}

// A frozen clock forces every checkpoint into the same timestamp, proving
// ordering does not depend on wall-clock separation.
func TestOrderingWithFrozenClock(t *testing.T) {
	fixed := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	s := newStore(t, WithClock(func() time.Time { return fixed }))
	ctx := context.Background()

	var ids []string
	for _, name := range []string{"a", "b", "c", "d"} {
		id, err := s.CreateCheckpoint(ctx, sampleState(name))
		if err != nil {
			t.Fatalf("CreateCheckpoint failed: %v", err)
		}
		ids = append(ids, id)
	}

	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate checkpoint id %q under a frozen clock", id)
		}
		seen[id] = true
	}

	got, err := s.GetLatestCheckpoint(ctx)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint failed: %v", err)
	}
	if got.TaskID != "d" {
		t.Errorf("newest should be d, got %q", got.TaskID)
	}
}

func TestListCheckpoints(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for _, name := range []string{"one", "two", "three"} {
		s.CreateCheckpoint(ctx, sampleState(name))
	}

	list, err := s.ListCheckpoints(ctx)
	if err != nil {
		t.Fatalf("ListCheckpoints failed: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d checkpoints, want 3", len(list))
	}

	if list[0].TaskID != "three" {
		t.Errorf("newest first expected, got %q", list[0].TaskID)
	}
	if list[0].Size <= 0 {
		t.Error("Size should be populated")
	}
	if list[0].Timestamp.IsZero() {
		t.Error("Timestamp should be populated")
	}
}

func TestListIgnoresForeignFiles(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	s.CreateCheckpoint(ctx, sampleState("real"))
	os.WriteFile(filepath.Join(s.dir, "notes.txt"), []byte("hi"), 0o644)
	os.Mkdir(filepath.Join(s.dir, "subdir"), 0o755)

	list, err := s.ListCheckpoints(ctx)
	if err != nil {
		t.Fatalf("ListCheckpoints failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("got %d entries, want 1 (foreign files must be ignored)", len(list))
	}
}

func TestDeleteCheckpoint(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	id, _ := s.CreateCheckpoint(ctx, sampleState("task"))

	if err := s.DeleteCheckpoint(ctx, id); err != nil {
		t.Fatalf("DeleteCheckpoint failed: %v", err)
	}
	if _, err := s.LoadCheckpoint(ctx, id); err == nil {
		t.Error("checkpoint should be gone after delete")
	}
	if err := s.DeleteCheckpoint(ctx, id); err == nil {
		t.Error("deleting a missing checkpoint should error")
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for _, name := range []string{"a", "b", "c", "d", "e"} {
		s.CreateCheckpoint(ctx, sampleState(name))
	}

	if err := s.PruneOldCheckpoints(ctx, 2); err != nil {
		t.Fatalf("PruneOldCheckpoints failed: %v", err)
	}

	list, _ := s.ListCheckpoints(ctx)
	if len(list) != 2 {
		t.Fatalf("got %d checkpoints after prune, want 2", len(list))
	}
	if list[0].TaskID != "e" || list[1].TaskID != "d" {
		t.Errorf("prune kept the wrong checkpoints: %q, %q", list[0].TaskID, list[1].TaskID)
	}
}

func TestPruneNoOpWhenUnderLimit(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	s.CreateCheckpoint(ctx, sampleState("only"))

	if err := s.PruneOldCheckpoints(ctx, 5); err != nil {
		t.Fatalf("PruneOldCheckpoints failed: %v", err)
	}

	list, _ := s.ListCheckpoints(ctx)
	if len(list) != 1 {
		t.Errorf("got %d checkpoints, want 1", len(list))
	}
}

func TestPruneRejectsZeroRetention(t *testing.T) {
	s := newStore(t)
	if err := s.PruneOldCheckpoints(context.Background(), 0); err == nil {
		t.Fatal("retention of 0 must be rejected: it would delete everything")
	}
}

// Retention must not discard a usable checkpoint while keeping a damaged one,
// or recovery could be left with nothing loadable.
func TestPruneDiscardsCorruptBeforeIntact(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	oldGood, _ := s.CreateCheckpoint(ctx, sampleState("old-good"))
	s.CreateCheckpoint(ctx, sampleState("mid"))
	newBad, _ := s.CreateCheckpoint(ctx, sampleState("new-bad"))

	os.WriteFile(s.pathFor(newBad), []byte("corrupt"), 0o644)

	if err := s.PruneOldCheckpoints(ctx, 2); err != nil {
		t.Fatalf("PruneOldCheckpoints failed: %v", err)
	}

	if _, err := s.LoadCheckpoint(ctx, oldGood); err != nil {
		t.Errorf("intact checkpoint was pruned in favour of a corrupt one: %v", err)
	}
	if _, err := s.LoadCheckpoint(ctx, newBad); err == nil {
		t.Error("corrupt checkpoint should have been pruned")
	}
}

func TestWriteLeavesNoTemporaryFiles(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		s.CreateCheckpoint(ctx, sampleState("task"))
	}

	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestLoadMissingCheckpoint(t *testing.T) {
	s := newStore(t)

	_, err := s.LoadCheckpoint(context.Background(), "ckpt-does-not-exist")
	if err == nil {
		t.Fatal("loading a missing checkpoint must error")
	}
	if errors.Is(err, ErrCorrupt) {
		t.Error("a missing checkpoint is not corruption")
	}
}

// Ids reach the filesystem as paths, so traversal attempts must be refused.
func TestRejectsPathTraversalIDs(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for _, id := range []string{"", "../escape", "sub/dir", `back\slash`, "a/../../b"} {
		if _, err := s.LoadCheckpoint(ctx, id); err == nil {
			t.Errorf("LoadCheckpoint accepted unsafe id %q", id)
		}
		if err := s.DeleteCheckpoint(ctx, id); err == nil {
			t.Errorf("DeleteCheckpoint accepted unsafe id %q", id)
		}
	}
}

func TestConcurrentCreates(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	const n = 25
	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i], errs[i] = s.CreateCheckpoint(ctx, sampleState("concurrent"))
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent create failed: %v", errs[i])
		}
		if seen[ids[i]] {
			t.Fatalf("duplicate id under concurrency: %s", ids[i])
		}
		seen[ids[i]] = true
	}

	list, _ := s.ListCheckpoints(ctx)
	if len(list) != n {
		t.Errorf("got %d checkpoints, want %d", len(list), n)
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	s.CreateCheckpoint(ctx, sampleState("seed"))

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.CreateCheckpoint(ctx, sampleState("writer"))
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.GetLatestCheckpoint(ctx)
			_, _ = s.ListCheckpoints(ctx)
		}()
	}
	wg.Wait()
}
