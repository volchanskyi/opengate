package protocol

import (
	"bytes"
	"compress/flate"
	"errors"
	"fmt"
	"io"

	"github.com/vmihailenco/msgpack/v5"
)

// Reading an alert's evidence back.
//
// The mirror of the agent's own encode, and deliberately the only way in: the
// codec is named on the row rather than assumed, so a reader that meets one it
// does not know says so instead of inflating the bytes and handing back
// whatever comes out. Evidence is frozen at write time and there is no path for
// asking the machine again, so a half-read blob would put invented detail in
// front of whoever is deciding what happened to a customer's machine.

const (
	// MaxEvidenceInflatedBytes bounds what compressed evidence may expand to
	// while it is being read. The composition is fixed — eight ranked
	// dimensions, three series of at most 512 points, ten processes, twenty log
	// lines — so a megabyte is orders of magnitude more than any honest blob
	// needs. What it refuses is the dishonest one: 64 KiB of DEFLATE can name
	// gigabytes of output, and inflating it to find out would be the reader
	// doing the endpoint's bidding.
	MaxEvidenceInflatedBytes = 1 << 20
)

var (
	// ErrUnknownEvidenceCodec is evidence compressed by something this build
	// does not read. It is its own error because it is the additive case working
	// as designed — a newer agent, an older server — rather than a broken blob.
	ErrUnknownEvidenceCodec = errors.New("unknown evidence codec")
	// ErrEvidenceTooLarge is a blob that expands past anything the fixed
	// composition could produce.
	ErrEvidenceTooLarge = errors.New("evidence expands beyond its bound")
)

// DecodeAlertEvidence reads evidence written by the agent's encoder.
//
// It answers exactly two ways: this is what the machine sent, or this cannot be
// read and here is which of the three reasons applies — a codec nobody knows, a
// blob that does not decompress or expands too far, or bytes that decompress to
// something that is not evidence.
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
