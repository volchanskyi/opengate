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
		_, payload, err := codec.ReadFrame(bytes.NewReader(data))
		if err == nil && len(payload) > MaxFrameSize {
			t.Fatalf("ReadFrame returned payload of %d bytes (max %d)", len(payload), MaxFrameSize)
		}
	})
}

func FuzzDecodeControl(f *testing.F) {
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
