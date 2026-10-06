package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shouldGenerateGolden returns true when GENERATE_GOLDEN=1.
func shouldGenerateGolden() bool {
	return os.Getenv("GENERATE_GOLDEN") == "1"
}

// goldenMeta is the schema of a testdata/golden/*.meta.json sidecar: a golden file's variant
// and the protocol version it was generated under.
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

// goldenWriteDir returns the committed testdata/golden tree in generate mode and a temp dir
// otherwise, so the generators always run without mutating tracked fixtures.
func goldenWriteDir(t *testing.T) string {
	t.Helper()
	if shouldGenerateGolden() {
		return goldenDir()
	}
	return t.TempDir()
}

// writeGoldenSidecar writes the .meta.json companion for a golden .bin file into dir.
func writeGoldenSidecar(t *testing.T, dir, binName, variant, format string) {
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
	metaPath := filepath.Join(dir, metaName)
	require.NoError(t, os.WriteFile(metaPath, data, 0o600))
}

// writeReverseGolden writes the Go-encoded form of one wire message as go_<variant>.bin into
// dir, for the Rust reverse verifier to decode.
func writeReverseGolden(t *testing.T, dir, variant string, encoded []byte) {
	t.Helper()
	require.NotEmpty(t, encoded, "%s: encoded golden must be non-empty", variant)

	name := "go_" + variant + ".bin"
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
	writeGoldenSidecar(t, dir, name, variant, "msgpack")
}

// goldenDeviceHourlyCeiling is the push fixture's per-machine alert allowance, distinct from
// the shipped default.
const goldenDeviceHourlyCeiling uint32 = 37

// goldenSeverities is cycled across the fixture's rules so all three travel.
var goldenSeverities = []AlertSeverity{
	AlertSeverityCritical,
	AlertSeverityWarning,
	AlertSeverityInfo,
}

func goldenAlertRules() []ThresholdRule {
	predicates := []RulePredicate{
		RulePredicateInstant,
		RulePredicateRate,
		RulePredicateWindowMax,
		RulePredicateWindowMean,
	}
	names := append([]string{}, RuleMetrics...)
	aliases := make([]string, 0, len(RuleMetricAliases))
	for alias := range RuleMetricAliases {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	names = append(names, aliases...)

	rules := make([]ThresholdRule, 0, len(names))
	for i, metric := range names {
		predicate := predicates[i%len(predicates)]
		window := uint32(0)
		if predicate != RulePredicateInstant {
			window = uint32(30 * (i%3 + 1))
		}
		rules = append(rules, ThresholdRule{
			ID: fmt.Sprintf("golden-rule-%02d", i),
			// Revisions start at 2, so a dropped or defaulted field cannot match.
			Version: uint32(i + 2),
			// Cycled severities keep a dropped field from reading as the decoder's default.
			Severity:    goldenSeverities[i%len(goldenSeverities)],
			Metric:      metric,
			Comparator:  AlertComparatorGte,
			Threshold:   float64(90 - i),
			Clear:       float64(80 - i),
			SustainSecs: uint32(30 * i),
			Predicate:   predicate,
			WindowSecs:  window,
		})
	}
	rules[0].All = []RuleTerm{{
		Metric:     "disk.queue_depth",
		Comparator: AlertComparatorGt,
		Threshold:  8,
		Clear:      4,
		Predicate:  RulePredicateWindowMax,
		WindowSecs: 60,
	}}
	return rules
}

func writeReverseControlFrame(t *testing.T, dir string, codec *Codec, variant string, msg *ControlMessage) {
	t.Helper()
	payload, err := codec.EncodeControl(msg)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, codec.WriteFrame(&buf, FrameControl, payload))
	writeReverseGolden(t, dir, variant, buf.Bytes())
}

func TestGenerateReverseGoldens(t *testing.T) {
	dir := goldenWriteDir(t)
	codec := &Codec{}

	writeReverseGolden(t, dir, "ping", []byte{FramePing})
	writeReverseGolden(t, dir, "pong", []byte{FramePong})

	writeReverseControlFrame(t, dir, codec, "control_heartbeat", &ControlMessage{
		Type:      MsgAgentHeartbeat,
		Timestamp: 1_700_000_000,
	})

	writeReverseControlFrame(t, dir, codec, "control_agent_register", &ControlMessage{
		Type:         MsgAgentRegister,
		Capabilities: []AgentCapability{CapRemoteDesktop, CapTerminal},
		Hostname:     "golden-test-host",
		OS:           "linux",
		Arch:         "amd64",
		Version:      "0.1.0",
	})

	writeReverseControlFrame(t, dir, codec, "control_session_request", &ControlMessage{
		Type:     MsgSessionRequest,
		Token:    SessionToken(goldenSessionToken),
		RelayURL: "wss://relay.example.com/relay",
		Permissions: &Permissions{
			Desktop: true, Terminal: true, FileRead: true, FileWrite: false, Input: true,
		},
	})

	writeReverseControlFrame(t, dir, codec, "control_chat_message", &ControlMessage{
		Type:   MsgChatMessage,
		Text:   "hello from the operator",
		Sender: "operator@example.com",
	})

	writeReverseControlFrame(t, dir, codec, "control_restart_agent", &ControlMessage{
		Type:   MsgRestartAgent,
		Reason: "restart requested from web UI",
	})

	writeReverseControlFrame(t, dir, codec, "control_agent_update", &ControlMessage{
		Type:      MsgAgentUpdate,
		Version:   "0.15.4",
		URL:       "https://updates.example.com/agent-0.15.4",
		SHA256:    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Signature: "3045022100deadbeef",
	})

	writeReverseControlFrame(t, dir, codec, "control_agent_deregistered", &ControlMessage{
		Type:   MsgAgentDeregistered,
		Reason: "device deleted",
	})

	// Every field but Type is omitempty, so these pin the smallest frame per variant.
	writeReverseControlFrame(t, dir, codec, "control_restart_agent_min", &ControlMessage{
		Type: MsgRestartAgent,
	})

	writeReverseControlFrame(t, dir, codec, "control_agent_deregistered_min", &ControlMessage{
		Type: MsgAgentDeregistered,
	})

	writeReverseControlFrame(t, dir, codec, "control_request_hardware_report", &ControlMessage{
		Type: MsgRequestHardwareReport,
	})

	writeReverseControlFrame(t, dir, codec, "control_request_device_logs", &ControlMessage{
		Type: MsgRequestDeviceLogs,
	})

	writeReverseControlFrame(t, dir, codec, "control_request_health_window", &ControlMessage{
		Type:    MsgRequestHealthWindow,
		SinceTS: 1_700_000_000,
		Limit:   12,
	})

	writeReverseControlFrame(t, dir, codec, "control_unknown_future_server_to_agent", &ControlMessage{
		Type: ControlMessageType("FutureHealthWindow"),
	})

	writeReverseControlFrame(t, dir, codec, "control_grant_backfill", &ControlMessage{
		Type:     MsgGrantBackfill,
		Rate:     500,
		Deadline: 1_700_003_600,
	})

	writeReverseControlFrame(t, dir, codec, "control_defer_backfill", &ControlMessage{
		Type:       MsgDeferBackfill,
		RetryAfter: 30,
	})

	writeReverseControlFrame(t, dir, codec, "control_metric_backfill_ack", &ControlMessage{
		Type:   MsgMetricBackfillAck,
		Tier:   BackfillTierRollup1m,
		Cursor: 1_700_000_060,
	})

	writeReverseControlFrame(t, dir, codec, "control_request_local_history", &ControlMessage{
		Type:      MsgRequestLocalHistory,
		Dim:       "cpu.total",
		FromTS:    1_699_990_000,
		ToTS:      1_700_000_000,
		MaxPoints: 1000,
	})

	// The ruleset is generated from the vocabulary, so the Rust decoder can compare it with its own.
	writeReverseControlFrame(t, dir, codec, "control_push_alert_rules", &ControlMessage{
		Type:                MsgPushAlertRules,
		AlertRules:          goldenAlertRules(),
		DeviceHourlyCeiling: goldenDeviceHourlyCeiling,
	})

	// Enabled is a *bool so false still emits the key that Rust always sends.
	{
		enabled := true
		writeReverseControlFrame(t, dir, codec, "control_set_maintenance_mode", &ControlMessage{
			Type:    MsgSetMaintenanceMode,
			Enabled: &enabled,
		})
	}

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
		writeReverseGolden(t, dir, "desktop_frame", buf.Bytes())
	}
}

func TestGenerateForwardSidecars(t *testing.T) {
	dir := goldenWriteDir(t)

	entries, err := os.ReadDir(goldenDir())
	require.NoError(t, err)

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".bin") {
			continue
		}
		if strings.HasPrefix(name, "go_") {
			continue
		}
		variant := strings.TrimSuffix(name, ".bin")
		format := "msgpack"
		// Handshake messages use a fixed binary layout, not msgpack.
		if strings.HasPrefix(variant, "handshake_") {
			format = "binary"
		}
		if variant == "ping" || variant == "pong" {
			format = "frame-only"
		}
		writeGoldenSidecar(t, dir, name, variant, format)
	}
}

func TestGoldenSidecarsExist(t *testing.T) {
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
