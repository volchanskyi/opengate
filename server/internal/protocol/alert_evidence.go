package protocol

import (
	"bytes"
	"compress/flate"
	"errors"
	"fmt"
	"io"

	"github.com/vmihailenco/msgpack/v5"
)

const (
	// MaxEvidenceInflatedBytes bounds what compressed evidence may expand to while it is read,
	// since DEFLATE can expand a blob about a thousandfold.
	MaxEvidenceInflatedBytes = 1 << 20
)

var (
	// ErrUnknownEvidenceCodec is evidence compressed by a codec this build does not read, as from
	// a newer agent.
	ErrUnknownEvidenceCodec = errors.New("unknown evidence codec")
	// ErrEvidenceTooLarge is a blob that expands past anything the fixed
	// composition could produce.
	ErrEvidenceTooLarge = errors.New("evidence expands beyond its bound")
)

// DecodeAlertEvidence reads evidence written by the agent's encoder, failing on an unknown
// codec, a blob that expands too far or does not decompress, or bytes that are not evidence.
func DecodeAlertEvidence(blob []byte, codec string) (AlertEvidence, error) {
	if codec != EvidenceCodec {
		return AlertEvidence{}, fmt.Errorf("%w: %q", ErrUnknownEvidenceCodec, codec)
	}

	reader := flate.NewReader(bytes.NewReader(blob))
	packed, err := io.ReadAll(io.LimitReader(reader, MaxEvidenceInflatedBytes+1))
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return AlertEvidence{}, fmt.Errorf("decompress evidence: %w", err)
	}
	if len(packed) > MaxEvidenceInflatedBytes {
		return AlertEvidence{}, fmt.Errorf("%w: over %d bytes", ErrEvidenceTooLarge, MaxEvidenceInflatedBytes)
	}

	var evidence AlertEvidence
	if err := msgpack.Unmarshal(packed, &evidence); err != nil {
		return AlertEvidence{}, fmt.Errorf("decode evidence: %w", err)
	}
	return evidence, nil
}
