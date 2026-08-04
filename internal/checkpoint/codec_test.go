package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

func TestEncodeRejectsUnserialisableState(t *testing.T) {
	// Channels have no JSON representation.
	if _, err := encode(map[string]any{"ch": make(chan int)}, false); err == nil {
		t.Fatal("unserialisable state must be rejected")
	}
}

func TestEncodeRejectsUnserialisableStateCompressed(t *testing.T) {
	if _, err := encode(map[string]any{"ch": make(chan int)}, true); err == nil {
		t.Fatal("unserialisable state must be rejected when compressing")
	}
}

func TestDecodeRejectsNewerFormatVersion(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{"a": "b"})
	env, _ := json.Marshal(envelope{
		Version:  formatVersion + 1,
		Checksum: checksum(payload),
		Payload:  payload,
	})

	err := decode(env, &map[string]string{})
	if err == nil {
		t.Fatal("a newer format version must be refused")
	}
	// A version this reader does not understand is not corruption; conflating
	// the two would let recovery silently discard a valid future checkpoint.
	if errors.Is(err, ErrCorrupt) {
		t.Error("newer format version should not be reported as corruption")
	}
	if !strings.Contains(err.Error(), "newer than supported") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecodeAcceptsCurrentVersion(t *testing.T) {
	data, err := encode(map[string]string{"k": "v"}, false)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	out := map[string]string{}
	if err := decode(data, &out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if out["k"] != "v" {
		t.Errorf("round trip lost data: %v", out)
	}
}

func TestDecodeEmptyPayload(t *testing.T) {
	env, _ := json.Marshal(envelope{Version: formatVersion, Checksum: checksum(nil)})

	if err := decode(env, &map[string]string{}); !errors.Is(err, ErrCorrupt) {
		t.Errorf("expected ErrCorrupt for empty payload, got %v", err)
	}
}

func TestDecodeMalformedEnvelope(t *testing.T) {
	if err := decode([]byte("not json at all"), &map[string]string{}); !errors.Is(err, ErrCorrupt) {
		t.Errorf("expected ErrCorrupt, got %v", err)
	}
}

func TestDecodePayloadShapeMismatch(t *testing.T) {
	// Checksum is valid, but the payload cannot populate the target type.
	payload := []byte(`"a bare string"`)
	env, _ := json.Marshal(envelope{
		Version:  formatVersion,
		Checksum: checksum(payload),
		Payload:  payload,
	})

	var out interfaces.CheckpointState
	if err := decode(env, &out); !errors.Is(err, ErrCorrupt) {
		t.Errorf("expected ErrCorrupt for payload/type mismatch, got %v", err)
	}
}

func TestGunzipRejectsNonGzip(t *testing.T) {
	if _, err := gunzipBytes([]byte("plain bytes")); err == nil {
		t.Fatal("non-gzip input must be rejected")
	}
}

func TestGunzipRejectsTruncatedStream(t *testing.T) {
	good, err := gzipBytes([]byte(strings.Repeat("payload", 100)))
	if err != nil {
		t.Fatalf("gzipBytes failed: %v", err)
	}

	if _, err := gunzipBytes(good[:len(good)/2]); err == nil {
		t.Fatal("truncated gzip stream must be rejected")
	}
}

func TestGzipRoundTrip(t *testing.T) {
	original := []byte(strings.Repeat("checkpoint payload ", 500))

	compressed, err := gzipBytes(original)
	if err != nil {
		t.Fatalf("gzipBytes failed: %v", err)
	}
	if len(compressed) >= len(original) {
		t.Errorf("compression did not shrink repetitive data: %d -> %d", len(original), len(compressed))
	}

	restored, err := gunzipBytes(compressed)
	if err != nil {
		t.Fatalf("gunzipBytes failed: %v", err)
	}
	if string(restored) != string(original) {
		t.Error("round trip altered the payload")
	}
}

func TestChecksumDetectsSingleByteChange(t *testing.T) {
	a := checksum([]byte("checkpoint"))
	b := checksum([]byte("checkpoinu"))

	if a == b {
		t.Error("checksum must differ for differing input")
	}
	if a != checksum([]byte("checkpoint")) {
		t.Error("checksum must be stable for identical input")
	}
}

func TestTimePartOfMalformedID(t *testing.T) {
	for _, id := range []string{"", "ckpt", "ckpt-only"} {
		if got := timePartOf(id); got != "" {
			t.Errorf("timePartOf(%q) = %q, want empty", id, got)
		}
	}
}

func TestCreateFailsOnUnwritableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	s := newStore(t)
	if err := os.Chmod(s.dir, 0o500); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}
	t.Cleanup(func() { os.Chmod(s.dir, 0o755) })

	_, err := s.CreateCheckpoint(context.Background(), sampleState("task"))
	if err == nil {
		t.Fatal("writing to a read-only directory must fail loudly, not silently succeed")
	}
}
