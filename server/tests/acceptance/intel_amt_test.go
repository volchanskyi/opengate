package acceptance

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// arrangeManagedIdentity gives a machine the Intel management identity its
// firmware would report. There is no operator door that creates one — the
// controller announces itself when it calls in — so the harness seeds it.
func (p *Product) arrangeManagedIdentity(machine *Machine) uuid.UUID {
	p.t.Helper()
	managed := testutil.SeedAMTDevice(p.t, arrangeTenantContext(), p.assembly.Store, machine.DeviceID)
	return managed.UUID
}

// TestATechnicianPowersOnAnUnresponsiveMachine is the sentence Intel AMT
// promises, and the one case where the thing at the far end genuinely cannot
// be real in a test: the management controller answers on its own network
// path, beside the operating system, which is the entire point of it.
func TestATechnicianPowersOnAnUnresponsiveMachine(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-till-1")
	machine.AwaitOnline()
	managed := product.arrangeManagedIdentity(machine)

	// The machine itself is unresponsive; its controller is calling in.
	machine.Disconnect()
	product.hardware.arrangeReachable(managed)

	reply := admin.Post("/api/v1/amt/devices/"+managed.String()+"/power",
		map[string]any{"action": "power_on"})
	require.Equalf(t, http.StatusOK, reply.Status, "powering the machine on failed: %s", reply.Text())

	require.Len(t, product.hardware.actions, 1, "one instruction reached the controller")
	assert.Equal(t, managed, product.hardware.actions[0].Device,
		"the instruction went to the machine the technician was looking at")
}

// TestPoweringOnAMachineWhoseControllerIsSilentSaysSo covers the ordinary
// case: the controller is not calling in, so nothing can be done and the
// technician has to be told which of the two it is.
func TestPoweringOnAMachineWhoseControllerIsSilentSaysSo(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-till-2")
	machine.AwaitOnline()
	managed := product.arrangeManagedIdentity(machine)

	reply := admin.Post("/api/v1/amt/devices/"+managed.String()+"/power",
		map[string]any{"action": "power_on"})
	assert.Equal(t, http.StatusConflict, reply.Status)
	assert.Empty(t, product.hardware.actions, "nothing is sent to a controller that is not there")
}

// TestPoweringOnAMachineInAnotherTenantIsNotFound keeps the one command that
// reaches hardware inside its tenant. The controller map is keyed by the
// management identity alone and carries no tenant of its own, so this lookup is
// the whole of the boundary.
func TestPoweringOnAMachineInAnotherTenantIsNotFound(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-till-3")
	machine.AwaitOnline()
	managed := product.arrangeManagedIdentity(machine)
	product.hardware.arrangeReachable(managed)

	outsider := product.TechnicianIn(product.arrangeSeparateTenant("Northwind"))
	reply := outsider.Post("/api/v1/amt/devices/"+managed.String()+"/power",
		map[string]any{"action": "power_on"})

	assert.Equal(t, http.StatusNotFound, reply.Status)
	assert.Empty(t, product.hardware.actions, "a command that is refused never reaches hardware")
}
