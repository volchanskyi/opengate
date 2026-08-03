package agentapi

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every server → agent control variant must have a reverse golden: a Go-encoded
// fixture the Rust harness decodes
// (agent/crates/mesh-protocol/tests/reverse_golden_test.rs). Without one, a
// variant whose wire shape the agent's decoder rejects ships unnoticed — the
// forward goldens only cover Rust-encode → Go-decode, the opposite direction.
//
// This guard reflects over *AgentConn's exported Send* methods and refuses any
// that this table does not name, so adding a server → agent write forces a
// golden alongside it. The three variants written inline from response handlers
// rather than through a Send* method are listed too, so the file-existence half
// covers the whole write surface.
var reverseGoldenByWrite = map[string][]string{
	"SendSessionRequest": {"go_control_session_request.bin"},
	"SendAgentUpdate":    {"go_control_agent_update.bin"},
	// Both informational-reason variants carry a _min fixture: the reason field
	// is dropped from the wire map when empty, and that shape must still decode.
	"SendAgentDeregistered":     {"go_control_agent_deregistered.bin", "go_control_agent_deregistered_min.bin"},
	"SendRestartAgent":          {"go_control_restart_agent.bin", "go_control_restart_agent_min.bin"},
	"SendRequestHardwareReport": {"go_control_request_hardware_report.bin"},
	"SendRequestHealthWindow":   {"go_control_request_health_window.bin"},
	"SendPushAlertRules":        {"go_control_push_alert_rules.bin"},
	"SendRequestLocalHistory":   {"go_control_request_local_history.bin"},
	"SendRequestDeviceLogs":     {"go_control_request_device_logs.bin"},
	"SendSetMaintenanceMode":    {"go_control_set_maintenance_mode.bin"},

	// Written inline by the backfill response handlers, so reflection does not
	// reach them; named here to keep their goldens under the same guard.
	"handleMetricBackfillBatch/ack": {"go_control_metric_backfill_ack.bin"},
	"handleRequestBackfillSlot/grant": {
		"go_control_grant_backfill.bin",
		"go_control_defer_backfill.bin",
	},
}

// goldenDir resolves the shared testdata/golden tree from this package.
func goldenDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "..", "testdata", "golden")
}

// TestEveryAgentWriteHasReverseGolden fails when a server → agent write is added
// without a reverse golden that proves the agent decodes its wire shape.
func TestEveryAgentWriteHasReverseGolden(t *testing.T) {
	t.Parallel()

	connType := reflect.TypeOf(&AgentConn{})
	var sends []string
	for i := 0; i < connType.NumMethod(); i++ {
		if name := connType.Method(i).Name; strings.HasPrefix(name, "Send") {
			sends = append(sends, name)
		}
	}
	require.NotEmpty(t, sends, "reflection found no Send* methods on *AgentConn")

	for _, name := range sends {
		assert.Contains(t, reverseGoldenByWrite, name,
			"%s writes to the agent with no reverse golden: add one in "+
				"TestGenerateReverseGoldens and verify it in reverse_golden_test.rs", name)
	}
}

// TestReverseGoldenTableResolvesToFiles fails when a golden the table names has
// been renamed or deleted, so the table cannot silently point at nothing.
func TestReverseGoldenTableResolvesToFiles(t *testing.T) {
	t.Parallel()

	for write, goldens := range reverseGoldenByWrite {
		require.NotEmpty(t, goldens, "%s: table entry names no golden", write)
		for _, golden := range goldens {
			info, err := os.Stat(filepath.Join(goldenDir(), golden))
			require.NoError(t, err, "%s: golden %s is missing", write, golden)
			assert.NotZero(t, info.Size(), "%s: golden %s is empty", write, golden)
		}
	}
}
