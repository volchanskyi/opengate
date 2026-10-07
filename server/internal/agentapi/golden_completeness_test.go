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

// reverseGoldenBySendMethod maps each Send* method to the Go-encoded fixtures the Rust
// harness decodes; the reflection guard fails on a Send* method it does not name.
var reverseGoldenBySendMethod = map[string][]string{
	"SendSessionRequest": {"go_control_session_request.bin"},
	"SendAgentUpdate":    {"go_control_agent_update.bin"},
	// The _min fixtures cover an empty reason, which the encoder drops from the wire map.
	"SendAgentDeregistered":     {"go_control_agent_deregistered.bin", "go_control_agent_deregistered_min.bin"},
	"SendRestartAgent":          {"go_control_restart_agent.bin", "go_control_restart_agent_min.bin"},
	"SendRequestHardwareReport": {"go_control_request_hardware_report.bin"},
	"SendRequestHealthWindow":   {"go_control_request_health_window.bin"},
	"SendPushAlertRules":        {"go_control_push_alert_rules.bin"},
	"SendRequestLocalHistory":   {"go_control_request_local_history.bin"},
	"SendRequestDeviceLogs":     {"go_control_request_device_logs.bin"},
	"SendSetMaintenanceMode":    {"go_control_set_maintenance_mode.bin"},
}

// reverseGoldenByInlineWrite names the goldens of variants written through sendControl
// directly, which reflection does not reach.
var reverseGoldenByInlineWrite = map[string][]string{
	"handleMetricBackfillBatch/ack": {"go_control_metric_backfill_ack.bin"},
	"handleRequestBackfillSlot/grant": {
		"go_control_grant_backfill.bin",
		"go_control_defer_backfill.bin",
	},
}

func goldenDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "..", "testdata", "golden")
}

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
		assert.Contains(t, reverseGoldenBySendMethod, name,
			"%s writes to the agent with no reverse golden: add one in "+
				"TestGenerateReverseGoldens and verify it in reverse_golden_test.rs", name)
	}
}

func TestReverseGoldenTableResolvesToFiles(t *testing.T) {
	t.Parallel()

	for _, table := range []map[string][]string{reverseGoldenBySendMethod, reverseGoldenByInlineWrite} {
		for write, goldens := range table {
			require.NotEmpty(t, goldens, "%s: table entry names no golden", write)
			for _, golden := range goldens {
				info, err := os.Stat(filepath.Join(goldenDir(), golden))
				require.NoError(t, err, "%s: golden %s is missing", write, golden)
				assert.NotZero(t, info.Size(), "%s: golden %s is empty", write, golden)
			}
		}
	}
}
