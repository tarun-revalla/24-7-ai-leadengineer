package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestWriteCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	if err := Write(path, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Errorf("contents: got %q", got)
	}
}

func TestWriteReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	Write(path, []byte("first"), 0o644)
	if err := Write(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "second" {
		t.Errorf("contents: got %q, want second", got)
	}
}

func TestWriteAppliesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics required")
	}

	path := filepath.Join(t.TempDir(), "state.json")

	// CreateTemp makes files 0600; the caller's mode must win, otherwise
	// every file this package writes would silently become owner-only.
	if err := Write(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("permissions: got %o, want 644", perm)
	}
}

func TestWriteLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	for i := 0; i < 10; i++ {
		if err := Write(path, []byte("data"), 0o644); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("expected exactly one file, got %d", len(entries))
	}
}

func TestWriteRejectsEmptyPath(t *testing.T) {
	if err := Write("", []byte("x"), 0o644); err == nil {
		t.Fatal("empty path must be rejected")
	}
}

func TestWriteFailsOnMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "state.json")

	if err := Write(path, []byte("x"), 0o644); err == nil {
		t.Fatal("writing into a missing directory must fail")
	}
}

func TestWriteFailsOnUnwritableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	if err := Write(filepath.Join(dir, "state.json"), []byte("x"), 0o644); err == nil {
		t.Fatal("writing to a read-only directory must fail loudly")
	}
}

// A reader must never observe a partially written file. Concurrent writers
// racing on one path should still leave a complete, readable result.
func TestConcurrentWritesLeaveCompleteContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	payload := strings.Repeat("0123456789", 500)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = Write(path, []byte(payload), 0o644)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := os.ReadFile(path); err == nil {
				if len(got) != 0 && string(got) != payload {
					t.Errorf("observed a partially written file: %d bytes", len(got))
				}
			}
		}()
	}
	wg.Wait()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("final read failed: %v", err)
	}
	if string(got) != payload {
		t.Errorf("final contents truncated: %d bytes", len(got))
	}
}

// The temp file must not survive a failure between its creation and the
// rename, or a crashing system slowly fills its state directory with debris.
func TestWriteCleansUpAfterAFailedRename(t *testing.T) {
	dir := t.TempDir()

	// A path whose final component is an existing directory cannot be renamed
	// over, so the rename fails after the temp file is fully written.
	target := filepath.Join(dir, "occupied")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("failed to create the blocking directory: %v", err)
	}

	if err := Write(target, []byte("content"), 0o644); err == nil {
		t.Fatal("writing over a directory should fail")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("a temporary file was left behind: %s", e.Name())
		}
	}
}

func TestWriteHandlesEmptyContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.txt")

	if err := Write(path, nil, 0o644); err != nil {
		t.Fatalf("writing empty content should succeed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("got %d bytes, want an empty file", len(data))
	}
}

// A file large enough to span several buffers must still arrive whole.
func TestWriteHandlesLargeContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.txt")
	content := []byte(strings.Repeat("abcdefgh", 200_000))

	if err := Write(path, content, 0o644); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if len(data) != len(content) {
		t.Errorf("got %d bytes, want %d", len(data), len(content))
	}
}

// syncDir deliberately swallows its errors: the file contents are already
// durable by the time it runs, so a platform that will not sync a directory
// must not turn a successful write into a failure.
func TestSyncDirIgnoresAMissingDirectory(t *testing.T) {
	syncDir(filepath.Join(t.TempDir(), "does-not-exist"))
}
