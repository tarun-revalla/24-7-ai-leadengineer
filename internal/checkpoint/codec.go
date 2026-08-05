package checkpoint

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// formatVersion is the on-disk envelope version. It is written with every
// checkpoint so a future reader can recognise and migrate older layouts
// instead of misparsing them.
const formatVersion = 1

// maxDecompressed caps how much a gzip payload may expand to. A checkpoint is
// kilobytes; anything vastly larger indicates corruption or a crafted file, and
// decompressing it unbounded would exhaust memory.
const maxDecompressed = 256 << 20 // 256 MiB

// ErrCorrupt indicates a checkpoint failed integrity verification. It is
// returned for malformed envelopes and checksum mismatches alike, so callers
// can treat "this file cannot be trusted" as one condition.
var ErrCorrupt = errors.New("checkpoint is corrupt")

// envelope wraps a checkpoint payload with the metadata needed to verify it.
// The checksum covers Payload only, so it stays valid regardless of how the
// surrounding fields are serialised.
type envelope struct {
	Version    int             `json:"version"`
	Checksum   string          `json:"checksum"`
	Compressed bool            `json:"compressed"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	// Blob holds the gzip-compressed payload when Compressed is set. JSON
	// cannot hold raw binary, so it is base64-encoded by encoding/json.
	Blob []byte `json:"blob,omitempty"`
}

// encode serialises state into a verifiable envelope, optionally compressing
// the payload.
func encode(state any, compress bool) ([]byte, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal checkpoint state: %w", err)
	}

	env := envelope{
		Version:  formatVersion,
		Checksum: checksum(payload),
	}

	if compress {
		blob, err := gzipBytes(payload)
		if err != nil {
			return nil, err
		}
		env.Compressed = true
		env.Blob = blob
	} else {
		env.Payload = payload
	}

	encoded, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal checkpoint envelope: %w", err)
	}

	return encoded, nil
}

// decode verifies an envelope and unmarshals its payload into out.
// Any failure to parse or verify is reported as ErrCorrupt so that callers
// scanning for a usable checkpoint can skip the file on a single condition.
func decode(data []byte, out any) error {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("%w: malformed envelope: %v", ErrCorrupt, err)
	}

	if env.Version > formatVersion {
		return fmt.Errorf("checkpoint format version %d is newer than supported version %d",
			env.Version, formatVersion)
	}

	payload := env.Payload
	if env.Compressed {
		var err error
		payload, err = gunzipBytes(env.Blob)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
	}

	if len(payload) == 0 {
		return fmt.Errorf("%w: empty payload", ErrCorrupt)
	}

	if got := checksum(payload); got != env.Checksum {
		return fmt.Errorf("%w: checksum mismatch (want %s, got %s)", ErrCorrupt, env.Checksum, got)
	}

	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("%w: malformed payload: %v", ErrCorrupt, err)
	}

	return nil
}

func checksum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func gzipBytes(in []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)

	if _, err := zw.Write(in); err != nil {
		return nil, fmt.Errorf("failed to compress checkpoint: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalise compressed checkpoint: %w", err)
	}

	return buf.Bytes(), nil
}

func gunzipBytes(in []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(in))
	if err != nil {
		return nil, fmt.Errorf("failed to open compressed checkpoint: %w", err)
	}
	// A failure closing the reader after a successful decompression is not
	// actionable — the bytes are already read.
	defer func() { _ = zr.Close() }()

	out, err := io.ReadAll(io.LimitReader(zr, maxDecompressed))
	if err != nil {
		return nil, fmt.Errorf("failed to decompress checkpoint: %w", err)
	}

	return out, nil
}
