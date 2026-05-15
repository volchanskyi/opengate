package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shouldGenerateGolden returns true when GENERATE_GOLDEN=1. Mirrors the Rust
// helper in agent/crates/mesh-protocol/tests/golden_test.rs.
func shouldGenerateGolden() bool {
	return os.Getenv("GENERATE_GOLDEN") == "1"
}

// goldenMeta is the on-disk schema for testdata/golden/*.meta.json sidecars.
// Each golden binary file gets one sidecar describing its variant and the
// protocol version it was generated under. Lays groundwork for future protocol
// version bumps (a v1 golden would coexist with v0 goldens, both verified).
type goldenMeta struct {
	Variant         string `json:"variant"`
	ProtocolVersion int    `json:"protocol_version"`
	Format          string `json:"format"`
	Created         string `json:"created"`
}

const (
	goldenProtocolVersion = 0
	goldenCreatedDate     = "2026-05-14"
)

// writeGoldenSidecar writes the .meta.json companion for a golden .bin file.
// Idempotent on identical input — re-running the generator overwrites with the
// same content unless the metadata schema changes.
func writeGoldenSidecar(t *testing.T, binName, variant, format string) {
	t.Helper()
	meta := goldenMeta{
		Variant:         variant,
		ProtocolVersion: goldenProtocolVersion,
		Format:          format,
		Created:         goldenCreatedDate,
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	data = append(data, '\n')

	metaName := strings.TrimSuffix(binName, ".bin") + ".meta.json"
	metaPath := filepath.Join(goldenDir(), metaName)
	require.NoError(t, os.WriteFile(metaPath, data, 0o600))
}

// writeReverseGolden constructs and writes a go_<variant>.bin file containing
// the Go-encoded form of one wire message. The Rust reverse verifier
// (agent/crates/mesh-protocol/tests/reverse_golden_test.rs) decodes these and
// asserts field equality against the canonical fixtures.
//
// Only runs when GENERATE_GOLDEN=1. Verification of the resulting files is the
// Rust side's job.
func writeReverseGolden(t *testing.T, variant string, encoded []byte) {
	t.Helper()
	name := "go_" + variant + ".bin"
	path := filepath.Join(goldenDir(), name)
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
	writeGoldenSidecar(t, name, variant, "msgpack")
}

// writeReverseControlFrame encodes msg via codec, wraps it in a FrameControl
// envelope, and writes the result as go_<variant>.bin.
func writeReverseControlFrame(t *testing.T, codec *Codec, variant string, msg *ControlMessage) {
	t.Helper()
	payload, err := codec.EncodeControl(msg)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, codec.WriteFrame(&buf, FrameControl, payload))
	writeReverseGolden(t, variant, buf.Bytes())
}

// TestGenerateReverseGoldens emits Go-encoded golden files when
// GENERATE_GOLDEN=1 is set. Otherwise it is a noop — the canonical (Rust-side)
// goldens are verified by the rest of golden_test.go.
//
// Covers a representative subset of wire-protocol variants: ping/pong, the
// most-used control messages, a nested struct (SessionRequest.Permissions),
// and a non-control frame (desktop). The pattern is straightforward to extend.
func TestGenerateReverseGoldens(t *testing.T) {
	if !shouldGenerateGolden() {
		t.Skip("set GENERATE_GOLDEN=1 to write go_*.bin reverse goldens")
	}

	codec := &Codec{}

	// Ping / Pong — single-byte frames, no payload.
	writeReverseGolden(t, "ping", []byte{FramePing})
	writeReverseGolden(t, "pong", []byte{FramePong})

	writeReverseControlFrame(t, codec, "control_heartbeat", &ControlMessage{
		Type:      MsgAgentHeartbeat,
		Timestamp: 1_700_000_000,
	})

	// control_agent_register — capabilities + UTF-8-safe ASCII identifiers.
	writeReverseControlFrame(t, codec, "control_agent_register", &ControlMessage{
		Type:         MsgAgentRegister,
		Capabilities: []AgentCapability{CapRemoteDesktop, CapTerminal},
		Hostname:     "golden-test-host",
		OS:           "linux",
		Arch:         "amd64",
		Version:      "0.1.0",
	})

	// control_session_request — exercises a nested struct (Permissions).
	writeReverseControlFrame(t, codec, "control_session_request", &ControlMessage{
		Type:     MsgSessionRequest,
		Token:    SessionToken(goldenSessionToken),
		RelayURL: "wss://relay.example.com/relay",
		Permissions: &Permissions{
			Desktop: true, Terminal: true, FileRead: true, FileWrite: false, Input: true,
		},
	})

	writeReverseControlFrame(t, codec, "control_chat_message", &ControlMessage{
		Type:   MsgChatMessage,
		Text:   "hello from the operator",
		Sender: "operator@example.com",
	})

	writeReverseControlFrame(t, codec, "control_restart_agent", &ControlMessage{
		Type:   MsgRestartAgent,
		Reason: "restart requested from web UI",
	})

	// desktop_frame — different frame type, exercises the byte-data payload.
	{
		f := &DesktopFrame{
			Sequence: 42,
			X:        10,
			Y:        20,
			Width:    1920,
			Height:   1080,
			Encoding: EncodingZstd,
			Data:     []byte{0xDE, 0xAD, 0xBE, 0xEF},
		}
		payload, err := codec.EncodeDesktopFrame(f)
		require.NoError(t, err)
		var buf bytes.Buffer
		require.NoError(t, codec.WriteFrame(&buf, FrameDesktop, payload))
		writeReverseGolden(t, "desktop_frame", buf.Bytes())
	}
}

// TestGenerateForwardSidecars writes a .meta.json companion for every existing
// Rust-side golden binary. Runs only when GENERATE_GOLDEN=1.
//
// Sidecars carry the protocol version and format hint so future protocol bumps
// can coexist with current goldens (e.g. v0_*.bin + v1_*.bin verified side by
// side). See the C1 plan in .claude/plans/archive/.
func TestGenerateForwardSidecars(t *testing.T) {
	if !shouldGenerateGolden() {
		t.Skip("set GENERATE_GOLDEN=1 to write .meta.json sidecars")
	}

	entries, err := os.ReadDir(goldenDir())
	require.NoError(t, err)

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".bin") {
			continue
		}
		// Reverse goldens write their own sidecar in writeReverseGolden.
		if strings.HasPrefix(name, "go_") {
			continue
		}
		variant := strings.TrimSuffix(name, ".bin")
		format := "msgpack"
		// Handshake messages use a fixed binary layout, not msgpack.
		if strings.HasPrefix(variant, "handshake_") {
			format = "binary"
		}
		// Pings are a single-byte frame with no payload.
		if variant == "ping" || variant == "pong" {
			format = "frame-only"
		}
		writeGoldenSidecar(t, name, variant, format)
	}
}

// TestGoldenSidecarsExist asserts every .bin file in testdata/golden has a
// .meta.json companion. Runs in verification mode (without GENERATE_GOLDEN).
func TestGoldenSidecarsExist(t *testing.T) {
	if shouldGenerateGolden() {
		t.Skip("generate mode: sidecars are being written, not verified")
	}

	entries, err := os.ReadDir(goldenDir())
	require.NoError(t, err)

	var bins []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".bin") {
			continue
		}
		bins = append(bins, entry.Name())
	}
	require.NotEmpty(t, bins, "no .bin goldens found in %s", goldenDir())

	for _, bin := range bins {
		t.Run(bin, func(t *testing.T) {
			metaName := strings.TrimSuffix(bin, ".bin") + ".meta.json"
			metaPath := filepath.Join(goldenDir(), metaName)
			data, err := os.ReadFile(metaPath)
			require.NoError(t, err, "missing sidecar for %s", bin)

			var meta goldenMeta
			require.NoError(t, json.Unmarshal(data, &meta), "invalid sidecar JSON for %s", bin)

			assert.NotEmpty(t, meta.Variant, "%s: variant must be non-empty", bin)
			assert.GreaterOrEqual(t, meta.ProtocolVersion, 0, "%s: protocol_version must be >= 0", bin)
			assert.NotEmpty(t, meta.Format, "%s: format must be non-empty", bin)
		})
	}
}
