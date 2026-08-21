package acceptance

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// answerLogPull is a machine handing back the lines it was asked for, one of
// them carrying a secret in the clear the way a real log does.
func (m *Machine) answerLogPull(secret string) {
	m.t.Helper()
	go func() {
		m.Await(protocol.MsgRequestDeviceLogs)
		m.Send(&protocol.ControlMessage{
			Type: protocol.MsgDeviceLogsResponse,
			LogEntries: []protocol.LogEntry{
				{Timestamp: "2026-04-01T12:00:00Z", Level: "INFO", Target: "app", Message: "login ok"},
				{Timestamp: "2026-04-01T12:00:01Z", Level: "WARN", Target: "auth",
					Message: "Authorization: Bearer " + secret + " rejected"},
			},
			TotalCount: 2,
		})
	}()
}

// TestATechnicianPullsALogAndTheSecretInItNeverReachesThem is the sentence
// Endpoint Logs promises: the pull waits on the machine, the lines come back
// redacted whatever the machine sent, nothing is stored, and somebody can see
// afterwards that the log was read.
func TestATechnicianPullsALogAndTheSecretInItNeverReachesThem(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-desk-01")
	machine.AwaitOnline()

	const secret = "abcdef0123456789"
	machine.answerLogPull(secret)

	var pulled struct {
		Entries []struct {
			Message string `json:"message"`
		} `json:"entries"`
		Total int `json:"total"`
	}
	reply := admin.Get("/api/v1/devices/" + machine.DeviceID.String() + "/logs")
	require.Equalf(t, http.StatusOK, reply.Status, "pulling the log failed: %s", reply.Text())
	reply.Into(&pulled)

	require.Len(t, pulled.Entries, 2)
	assert.Equal(t, 2, pulled.Total)
	assert.NotContains(t, pulled.Entries[1].Message, secret,
		"the secret is stripped here, even though the machine sent it in the clear")
	assert.Contains(t, pulled.Entries[1].Message, "[REDACTED]")

	require.Eventually(t, func() bool {
		var events []struct {
			Action string `json:"action"`
			Target string `json:"target"`
		}
		admin.Get("/api/v1/audit?action=device.logs.read&limit=10").Into(&events)
		return len(events) >= 1 && events[0].Target == machine.DeviceID.String()
	}, eventually, poll, "reading somebody's machine's log is itself a recorded act")
}

// TestATechnicianWithoutElevatedPermissionCannotPullALog pins the gate in
// front of the machine round trip. A log is the most revealing thing a
// technician can ask a machine for, so the refusal happens before the machine
// is troubled at all.
func TestATechnicianWithoutElevatedPermissionCannotPullALog(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-desk-02")
	machine.AwaitOnline()

	viewer := product.Technician(contoso)
	assert.Equal(t, http.StatusForbidden,
		viewer.Get("/api/v1/devices/"+machine.DeviceID.String()+"/logs").Status)
	assert.False(t, machine.Received(protocol.MsgRequestDeviceLogs),
		"a refused pull never reaches the machine")
}
