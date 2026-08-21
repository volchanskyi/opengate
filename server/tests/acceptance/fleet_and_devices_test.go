package acceptance

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fleetSummary is the count strip at the top of the dashboard.
type fleetSummary struct {
	Total   int `json:"total"`
	Online  int `json:"online"`
	Offline int `json:"offline"`
}

// dashboard is the summary a technician's fleet page opens on.
func (a *Technician) dashboard() fleetSummary {
	a.t.Helper()
	var summary fleetSummary
	reply := a.Get(a.InCustomer("/api/v1/devices/summary"))
	require.Equalf(a.t, http.StatusOK, reply.Status, "reading the dashboard failed: %s", reply.Text())
	reply.Into(&summary)
	return summary
}

// fileUnder moves a machine to a customer, which is what a technician does
// when a machine arrives in the wrong place.
func (a *Technician) fileUnder(machine *Machine, customer any) Reply {
	a.t.Helper()
	return a.Put("/api/v1/devices/"+machine.DeviceID.String()+"/organization",
		map[string]any{"organization_id": customer})
}

// TestTheDashboardAgreesWithTheDeviceList is the sentence Fleet and Devices
// promises: what the counts say and what the list shows are the same estate.
// Handler tests prove each against a stub reader; only standing the product up
// proves they agree about a machine that actually connected.
func TestTheDashboardAgreesWithTheDeviceList(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	token := admin.mintEnrolmentToken("Head Office").Token

	online := product.Machine(token, "contoso-desk-01")
	online.AwaitOnline()
	departed := product.Machine(token, "contoso-desk-02")
	departed.AwaitOnline()
	departed.Disconnect()

	require.Eventually(t, func() bool {
		return admin.dashboard().Offline == 1
	}, eventually, poll, "a machine that left the network is offline on the dashboard")

	summary := admin.dashboard()
	assert.Equal(t, 2, summary.Total)
	assert.Equal(t, 1, summary.Online)
	assert.Len(t, admin.devices(), summary.Total,
		"the count strip and the list below it describe one estate")
}

// TestATechnicianSeesOneCustomersMachinesAtATime is the silent failure the
// customer filter exists to prevent. Both customers sit inside one tenant, so
// nothing is refused — a wrong query simply shows one customer's estate to
// somebody looking at another's, and nobody notices.
func TestATechnicianSeesOneCustomersMachinesAtATime(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	fabrikam := product.arrangeCustomer("Fabrikam")
	admin := product.Administrator(contoso)
	token := admin.mintEnrolmentToken("rollout").Token

	contosoMachine := product.Machine(token, "contoso-desk-01")
	contosoMachine.AwaitOnline()
	fabrikamMachine := product.Machine(token, "fabrikam-desk-01")
	fabrikamMachine.AwaitOnline()
	require.Equal(t, http.StatusOK, admin.fileUnder(fabrikamMachine, fabrikam).Status)

	looking := product.Administrator(fabrikam)
	fabrikamList := looking.devices()
	require.Len(t, fabrikamList, 1)
	assert.Equal(t, fabrikamMachine.DeviceID, fabrikamList[0].ID,
		"Fabrikam's page shows Fabrikam's machine")

	contosoList := admin.devices()
	require.Len(t, contosoList, 1)
	assert.Equal(t, contosoMachine.DeviceID, contosoList[0].ID,
		"and Contoso's page does not show it")
	assert.Equal(t, 1, looking.dashboard().Total,
		"the counts narrow with the list, or the dashboard describes somebody else's estate")
}
