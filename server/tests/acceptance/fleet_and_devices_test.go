package acceptance

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
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

// fileUnder moves a machine to a customer.
func (a *Technician) fileUnder(machine *Machine, customer any) Reply {
	a.t.Helper()
	return a.Put("/api/v1/devices/"+machine.DeviceID.String()+"/organization",
		map[string]any{"organization_id": customer})
}

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

func TestNotAssignedListsOnlyTheMachinesWithNoSite(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	token := admin.mintEnrolmentToken("Head Office").Token

	frontDesk := product.Machine(token, "contoso-front-desk")
	frontDesk.AwaitOnline()
	loose := product.Machine(token, "contoso-spare-laptop")
	loose.AwaitOnline()

	var site struct {
		ID uuid.UUID `json:"id"`
	}
	created := admin.Post("/api/v1/sites", map[string]any{"name": "Front Desk", "organization_id": contoso})
	require.Equalf(t, http.StatusCreated, created.Status, "creating a site failed: %s", created.Text())
	created.Into(&site)
	filed := admin.Patch("/api/v1/devices/"+frontDesk.DeviceID.String(), map[string]any{"site_id": site.ID})
	require.Equalf(t, http.StatusOK, filed.Status, "filing a machine failed: %s", filed.Text())

	var unfiled []deviceSummary
	reply := admin.Get(admin.InCustomer("/api/v1/devices?without_site=true"))
	require.Equalf(t, http.StatusOK, reply.Status, "listing machines with no site failed: %s", reply.Text())
	reply.Into(&unfiled)
	require.Len(t, unfiled, 1)
	assert.Equal(t, loose.DeviceID, unfiled[0].ID)

	both := admin.Get(admin.InCustomer("/api/v1/devices?without_site=true&site_id=" + site.ID.String()))
	assert.Equal(t, http.StatusBadRequest, both.Status, "a machine cannot be in a site and in none")
}

// listed is the row the device list shows for one machine.
func (a *Technician) listed(machine *Machine) deviceSummary {
	a.t.Helper()
	for _, d := range a.devices() {
		if d.ID == machine.DeviceID {
			return d
		}
	}
	require.FailNowf(a.t, "machine missing from the device list", "%s", machine.Hostname)
	return deviceSummary{}
}

func TestAMachineThatDisconnectsShowsOfflineWithTheTimeItLeft(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	laptop := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-laptop-07")
	laptop.AwaitOnline()
	require.Equal(t, "online", admin.listed(laptop).Status)

	left := time.Now()
	laptop.Disconnect()
	require.Eventually(t, func() bool {
		return admin.listed(laptop).Status == "offline"
	}, eventually, poll, "a machine that left the network shows offline")
	assert.WithinDuration(t, left, admin.listed(laptop).LastSeen, 5*time.Second,
		"the list shows the time the machine left")

	laptop.Reconnect()
	require.Eventually(t, func() bool {
		return admin.listed(laptop).Status == "online"
	}, eventually, poll, "a machine that comes back shows online again")
}
