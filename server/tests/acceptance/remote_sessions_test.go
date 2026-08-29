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

// TestATechnicianOpensATerminalAndTheMachineIsToldToStartIt is the sentence
// Remote Sessions promises. A technician asks for a terminal on a machine that
// is online, and the machine hears about it on its own control stream — which
// is the whole point of the seam: the session the API mints and the session the
// machine is asked to start are the same session.
func TestATechnicianOpensATerminalAndTheMachineIsToldToStartIt(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-desk-01")
	machine.AwaitOnline()

	var session struct {
		Token    string `json:"token"`
		RelayURL string `json:"relay_url"`
	}
	reply := admin.Post("/api/v1/sessions", map[string]any{"device_id": machine.DeviceID.String()})
	require.Equalf(t, http.StatusCreated, reply.Status, "opening a terminal failed: %s", reply.Text())
	reply.Into(&session)
	require.NotEmpty(t, session.Token)
	assert.NotEmpty(t, session.RelayURL, "the browser is told where to connect")

	request := machine.Await(protocol.MsgSessionRequest)
	assert.Equal(t, session.Token, string(request.Token),
		"the machine is asked to start the very session the technician was given")
}

// TestASessionForAMachineThatIsOfflineIsRefusedWithAReason covers the case a
// technician meets every day: the machine is not there. The refusal has to say
// so, because a technician who is told nothing will keep clicking.
func TestASessionForAMachineThatIsOfflineIsRefusedWithAReason(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-desk-02")
	machine.AwaitOnline()
	machine.Disconnect()

	require.Eventually(t, func() bool {
		return admin.Post("/api/v1/sessions",
			map[string]any{"device_id": machine.DeviceID.String()}).Status == http.StatusConflict
	}, eventually, poll, "a terminal on a machine that is not there is refused")

	// Which refusal arrives depends on how far the request got before the
	// machine's departure was noticed — the connection is gone, or the write to
	// it failed — and both are true. What must hold either way is that the
	// answer names the machine rather than blaming the technician.
	reply := admin.Post("/api/v1/sessions", map[string]any{"device_id": machine.DeviceID.String()})
	assert.Equal(t, http.StatusConflict, reply.Status)
	assert.Containsf(t, reply.Text(), "agent",
		"the refusal names the reason a technician can act on, got %s", reply.Text())
}

// TestASessionOnAMachineThatDisappearsStopsBeingUsable is the technician
// mid-repair when the endpoint drops off the network. The session cannot
// survive the machine: nothing further is asked of it, a fresh terminal is
// refused with a reason, and the technician can clear the dead session from
// the page themselves rather than being left looking at it.
func TestASessionOnAMachineThatDisappearsStopsBeingUsable(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-desk-03")
	machine.AwaitOnline()

	var session struct {
		Token string `json:"token"`
	}
	reply := admin.Post("/api/v1/sessions", map[string]any{"device_id": machine.DeviceID.String()})
	require.Equal(t, http.StatusCreated, reply.Status)
	reply.Into(&session)
	machine.Await(protocol.MsgSessionRequest)

	machine.Disconnect()

	require.Eventually(t, func() bool {
		return admin.Post("/api/v1/sessions",
			map[string]any{"device_id": machine.DeviceID.String()}).Status == http.StatusConflict
	}, eventually, poll, "a machine that has gone can carry no further work")

	assert.Equal(t, http.StatusNoContent, admin.Delete("/api/v1/sessions/"+session.Token).Status,
		"the technician can clear a session whose machine is gone")
}

// TestASessionLeftByAMachineThatWentAwayIsReclaimed is the other half of the
// same story, and the half a technician never presses a button for. A session
// whose machine dropped off and whose technician closed the tab is nobody's
// job to clear, so the product clears it: once the row has outlived its grace
// period the sweep takes it, and it stops appearing on the machine's page.
//
// The sweep runs on a cadence measured in minutes in the running server, so
// this states its own — the point is that the product reclaims the row, not how
// long it waits first.
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
	}))
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-desk-05")
	machine.AwaitOnline()

	reply := admin.Post("/api/v1/sessions", map[string]any{"device_id": machine.DeviceID.String()})
	require.Equal(t, http.StatusCreated, reply.Status)
	machine.Await(protocol.MsgSessionRequest)

	// Nobody connects to it, and the machine goes away.
	machine.Disconnect()

	require.Eventually(t, func() bool {
		return len(admin.sessionsOn(machine.DeviceID)) == 0
	}, eventually, poll, "a session nobody is holding stops being one the machine has")
}

// sessionsOn is the session list a technician sees on a machine's page.
func (a *Technician) sessionsOn(deviceID uuid.UUID) []sessionSummary {
	a.t.Helper()
	var listed []sessionSummary
	reply := a.Get("/api/v1/sessions?device_id=" + deviceID.String())
	require.Equalf(a.t, http.StatusOK, reply.Status, "reading the session list failed: %s", reply.Text())
	reply.Into(&listed)
	return listed
}

// sessionSummary is a session as the machine's page shows it.
type sessionSummary struct {
	Token string `json:"token"`
}

// TestACustomerFilterNarrowsAndDoesNotPermit states the shape of the
// customer boundary out loud, because it is easy to mistake for a permission.
// Both customers sit inside one tenant, so a technician looking at Fabrikam is
// shown Fabrikam's estate — but nothing about naming Contoso's machine is
// refused. That is a filter, not a gate, and a test that assumed otherwise
// would be pinning a protection the product does not offer.
func TestACustomerFilterNarrowsAndDoesNotPermit(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	fabrikam := product.arrangeCustomer("Fabrikam")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-desk-04")
	machine.AwaitOnline()

	looking := product.Administrator(fabrikam)
	assert.Empty(t, looking.devices(), "Contoso's machine is not on Fabrikam's page")

	reply := looking.Post("/api/v1/sessions", map[string]any{"device_id": machine.DeviceID.String()})
	assert.Equal(t, http.StatusCreated, reply.Status,
		"the customer filter narrows what is shown; the tenant is what permits")
}
