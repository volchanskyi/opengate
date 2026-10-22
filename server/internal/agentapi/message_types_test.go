package agentapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// allControlMessageTypes is every control message type a restricted connection is tried with.
var allControlMessageTypes = []protocol.ControlMessageType{
	protocol.MsgAgentRegister, protocol.MsgAgentHeartbeat, protocol.MsgAgentHealthSummary,
	protocol.MsgAgentMetricWindow, protocol.MsgSessionAccept, protocol.MsgSessionReject,
	protocol.MsgSessionRequest, protocol.MsgAgentUpdate, protocol.MsgAgentUpdateAck,
	protocol.MsgRelayReady, protocol.MsgDirectOffer, protocol.MsgDirectAnswer,
	protocol.MsgIceCandidate, protocol.MsgDirectRefused, protocol.MsgDirectSwitch,
	protocol.MsgDirectFallback, protocol.MsgDirectAck, protocol.MsgMouseMove,
	protocol.MsgMouseClick, protocol.MsgKeyPress, protocol.MsgTerminalResize,
	protocol.MsgFileListRequest, protocol.MsgFileListResponse, protocol.MsgFileListError,
	protocol.MsgFileDownloadRequest, protocol.MsgFileUploadRequest, protocol.MsgFileDownloadError,
	protocol.MsgChatMessage, protocol.MsgAgentDeregistered, protocol.MsgRestartAgent,
	protocol.MsgRequestHardwareReport, protocol.MsgHardwareReport, protocol.MsgHardwareReportError,
	protocol.MsgRequestDeviceLogs, protocol.MsgDeviceLogsResponse, protocol.MsgDeviceLogsError,
	protocol.MsgRequestHealthWindow, protocol.MsgHealthWindowResponse, protocol.MsgRequestBackfillSlot,
	protocol.MsgGrantBackfill, protocol.MsgDeferBackfill, protocol.MsgMetricBackfillBatch,
	protocol.MsgMetricBackfillAck, protocol.MsgRequestLocalHistory, protocol.MsgLocalHistoryResponse,
	protocol.MsgDiscoveryReport, protocol.MsgPushAlertRules, protocol.MsgSetMaintenanceMode,
	protocol.MsgMaintenanceApplied, protocol.MsgAgentAlert, protocol.MsgRenewCertificate,
	protocol.MsgCertificateSigningRequest, protocol.MsgCertificateIssued, protocol.MsgCertificateRefused,
	protocol.MsgCertificateInstalled, protocol.MsgTrustBundle,
}

// controlMessageTypes reads every ControlMessageType constant the protocol package declares.
func controlMessageTypes(t *testing.T) []string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(here), "..", "protocol", "control.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	require.NoError(t, err)
	var names []string
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		if ident, ok := spec.Type.(*ast.Ident); ok && ident.Name == "ControlMessageType" {
			for _, name := range spec.Names {
				if strings.HasPrefix(name.Name, "Msg") {
					names = append(names, name.Name)
				}
			}
		}
		return true
	})
	require.NotEmpty(t, names, "no message types found in the protocol package")
	return names
}
