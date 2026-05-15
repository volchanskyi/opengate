package protocol

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forEachGolden invokes fn for every testdata/golden/*.bin file. Shared by
// both fuzz targets to centralize the directory walk and error handling.
func forEachGolden(f *testing.F, fn func(data []byte)) {
	f.Helper()
	entries, err := os.ReadDir(goldenDir())
	if err != nil {
		f.Fatalf("read goldenDir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".bin") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(goldenDir(), entry.Name()))
		if err != nil {
			f.Fatalf("read %s: %v", entry.Name(), err)
		}
		fn(data)
	}
}

// FuzzReadFrame fuzzes the wire-envelope parser. The contract is "do not
// panic, do not allocate beyond MaxFrameSize, return a recognizable error on
// malformed input". The fuzzer explores around the seed corpus of real
// protocol frames, looking for inputs that violate the contract.
//
// Run locally: `go test -fuzz=FuzzReadFrame -fuzztime=30s ./server/internal/protocol/`
func FuzzReadFrame(f *testing.F) {
	forEachGolden(f, func(data []byte) { f.Add(data) })
	// Hand-crafted edge cases — empty, truncated headers, header-only.
	f.Add([]byte{})
	f.Add([]byte{0x01})                         // type byte only, no length
	f.Add([]byte{0x01, 0x00, 0x00, 0x00, 0x00}) // header with zero-length payload
	f.Add([]byte{0xFF, 0x00, 0x00, 0x00, 0x00}) // unknown frame type
	f.Add([]byte{0x05})                         // bare ping
	f.Add([]byte{0x06})                         // bare pong

	f.Fuzz(func(t *testing.T, data []byte) {
		codec := &Codec{}
		// We do not assert on the success/error outcome — fuzz contract is
		// "no panic, no OOM". If a payload claims a length greater than
		// MaxFrameSize the codec returns ErrFrameTooLarge before allocating.
		_, payload, err := codec.ReadFrame(bytes.NewReader(data))
		// Sanity invariant: on success the payload size is bounded.
		if err == nil && len(payload) > MaxFrameSize {
			t.Fatalf("ReadFrame returned payload of %d bytes (max %d)", len(payload), MaxFrameSize)
		}
	})
}

// FuzzDecodeControl fuzzes the msgpack control-message decoder. The contract
// is "any byte sequence decodes successfully or returns an error — never
// panic". Crashes from this fuzzer most often indicate an unchecked nil
// dereference or a panic inside the msgpack library.
//
// Run locally: `go test -fuzz=FuzzDecodeControl -fuzztime=30s ./server/internal/protocol/`
func FuzzDecodeControl(f *testing.F) {
	// Seed corpus: payload sections extracted from forward goldens. We use
	// ReadFrame to peel the envelope so the fuzzer starts from msgpack-shaped
	// inputs, not the wire frame.
	codec := &Codec{}
	forEachGolden(f, func(data []byte) {
		// Only control frames carry a msgpack payload; skip the rest.
		if len(data) == 0 || data[0] != FrameControl {
			return
		}
		_, payload, err := codec.ReadFrame(bytes.NewReader(data))
		if err != nil {
			return
		}
		f.Add(payload)
	})
	// Hand-crafted: empty, single-byte, malformed msgpack.
	f.Add([]byte{})
	f.Add([]byte{0xC0}) // msgpack nil
	f.Add([]byte{0x80}) // msgpack empty fixmap

	f.Fuzz(func(t *testing.T, data []byte) {
		codec := &Codec{}
		// No assertion — contract is "do not panic".
		_, _ = codec.DecodeControl(data)
	})
}
