package acceptance

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// arrangeManagedIdentity seeds the Intel management identity that the controller announces
// when it calls in.
func (p *Product) arrangeManagedIdentity(machine *Machine) uuid.UUID {
	p.t.Helper()
	managed := testutil.SeedAMTDevice(p.t, arrangeTenantContext(), p.assembly.Store, machine.DeviceID)
	return managed.UUID
}

func TestATechnicianPowersOnAnUnresponsiveMachine(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-till-1")
	machine.AwaitOnline()
	managed := product.arrangeManagedIdentity(machine)

	// The machine's agent is gone while its controller stays reachable.
	machine.Disconnect()
	product.hardware.arrangeReachable(managed)

	reply := admin.Post("/api/v1/amt/devices/"+managed.String()+"/power",
		map[string]any{"action": "power_on"})
	require.Equalf(t, http.StatusOK, reply.Status, "powering the machine on failed: %s", reply.Text())

	require.Len(t, product.hardware.actions, 1, "one instruction reached the controller")
	assert.Equal(t, managed, product.hardware.actions[0].Device,
		"the instruction went to the machine the technician was looking at")
}

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
