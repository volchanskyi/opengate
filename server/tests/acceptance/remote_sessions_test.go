package acceptance

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/app"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func contosoOnlineMachine(t *testing.T, product *Product, hostname string) (*Technician, *Machine) {
	t.Helper()
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, hostname)
	machine.AwaitOnline()
	return admin, machine
}

func (a *Technician) openSession(deviceID uuid.UUID) Reply {
	a.t.Helper()
	return a.Post("/api/v1/sessions", map[string]any{"device_id": deviceID.String()})
}

func requireSessionsRefused(t *testing.T, admin *Technician, deviceID uuid.UUID, msg string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return admin.openSession(deviceID).Status == http.StatusConflict
	}, eventually, poll, msg)
}

func TestATechnicianOpensATerminalAndTheMachineIsToldToStartIt(t *testing.T) {
	t.Parallel()

	admin, machine := contosoOnlineMachine(t, newProduct(t), "contoso-desk-01")

	var session struct {
		Token    string `json:"token"`
		RelayURL string `json:"relay_url"`
	}
	reply := admin.openSession(machine.DeviceID)
	require.Equalf(t, http.StatusCreated, reply.Status, "opening a terminal failed: %s", reply.Text())
	reply.Into(&session)
	require.NotEmpty(t, session.Token)
	assert.NotEmpty(t, session.RelayURL, "the browser is told where to connect")

	request := machine.Await(protocol.MsgSessionRequest)
	assert.Equal(t, session.Token, string(request.Token),
		"the machine is asked to start the very session the technician was given")
}

func TestASessionForAMachineThatIsOfflineIsRefusedWithAReason(t *testing.T) {
	t.Parallel()

	admin, machine := contosoOnlineMachine(t, newProduct(t), "contoso-desk-02")
	machine.Disconnect()

	requireSessionsRefused(t, admin, machine.DeviceID, "a terminal on a machine that is not there is refused")

	// Either refusal wording is valid, depending on whether the departure was noticed first.
	reply := admin.openSession(machine.DeviceID)
	assert.Equal(t, http.StatusConflict, reply.Status)
	assert.Containsf(t, reply.Text(), "agent",
		"the refusal names the reason a technician can act on, got %s", reply.Text())
}

func TestASessionOnAMachineThatDisappearsStopsBeingUsable(t *testing.T) {
	t.Parallel()

	admin, machine := contosoOnlineMachine(t, newProduct(t), "contoso-desk-03")

	var session struct {
		Token string `json:"token"`
	}
	reply := admin.openSession(machine.DeviceID)
	require.Equal(t, http.StatusCreated, reply.Status)
	reply.Into(&session)
	machine.Await(protocol.MsgSessionRequest)

	machine.Disconnect()

	requireSessionsRefused(t, admin, machine.DeviceID, "a machine that has gone can carry no further work")

	assert.Equal(t, http.StatusNoContent, admin.Delete("/api/v1/sessions/"+session.Token).Status,
		"the technician can clear a session whose machine is gone")
}

func TestASessionLeftByAMachineThatWentAwayIsReclaimed(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithSweeps(app.BackgroundSchedule{
		Gauges:         time.Second,
		DBSize:         time.Second,
		Investigations: time.Second,
		Reconcile:      time.Hour,
		SessionSweep:   50 * time.Millisecond,
		SessionGrace:   time.Nanosecond,
		IncidentSweep:  time.Hour,
		// The retention horizon exceeds anything this run creates, leaving the age sweep idle.
		RetentionSweep:   time.Hour,
		RetentionHorizon: 365 * 24 * time.Hour,
	}))
	admin, machine := contosoOnlineMachine(t, product, "contoso-desk-05")

	reply := admin.openSession(machine.DeviceID)
	require.Equal(t, http.StatusCreated, reply.Status)
	machine.Await(protocol.MsgSessionRequest)

	machine.Disconnect()

	require.Eventually(t, func() bool {
		return len(admin.sessionsOn(machine.DeviceID)) == 0
	}, eventually, poll, "a session nobody is holding stops being one the machine has")
}

// sessionsOn lists the sessions of one machine.
func (a *Technician) sessionsOn(deviceID uuid.UUID) []sessionSummary {
	a.t.Helper()
	var listed []sessionSummary
	reply := a.Get("/api/v1/sessions?device_id=" + deviceID.String())
	require.Equalf(a.t, http.StatusOK, reply.Status, "reading the session list failed: %s", reply.Text())
	reply.Into(&listed)
	return listed
}

// sessionSummary is a session as the session list shows it.
type sessionSummary struct {
	Token string `json:"token"`
}

func TestACustomerFilterNarrowsAndDoesNotPermit(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	_, machine := contosoOnlineMachine(t, product, "contoso-desk-04")
	fabrikam := product.arrangeCustomer("Fabrikam")

	looking := product.Administrator(fabrikam)
	assert.Empty(t, looking.devices(), "Contoso's machine is not on Fabrikam's page")

	reply := looking.openSession(machine.DeviceID)
	assert.Equal(t, http.StatusCreated, reply.Status,
		"the customer filter narrows what is shown; the tenant is what permits")
}
