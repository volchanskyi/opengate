package acceptance

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestDeletingAMachineRemovesItAndStopsTrustingItsAgent(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	token := admin.mintEnrolmentToken("Head Office").Token

	machine := product.Machine(token, "contoso-desk-01")
	machine.AwaitOnline()

	reportedAt := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	machine.report(reportedAt, protocol.MetricDim{Name: "cpu.total", Avg: 33.5})
	machine.settle()
	from, to := reportedAt.Add(-5*time.Minute), reportedAt.Add(5*time.Minute)
	admin.awaitReading(product, machine.DeviceID, from, to, "cpu.total")

	require.Equal(t, http.StatusNoContent,
		admin.Delete("/api/v1/devices/"+machine.DeviceID.String()).Status)

	assert.Empty(t, admin.devices(), "an erased machine is off the fleet page")
	assert.Equal(t, http.StatusNotFound,
		admin.Get("/api/v1/devices/"+machine.DeviceID.String()).Status)

	chart := admin.Get("/api/v1/devices/" + machine.DeviceID.String() + "/metrics" +
		"?from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339) + "&dims=cpu.total")
	assert.Equal(t, http.StatusNotFound, chart.Status,
		"the readings are unreachable with the machine, or the erasure was not one")

	// The agent still holds its certificate, and is told to remove itself when it returns.
	product.assertToldToRemoveItself(machine, false)
}

func TestDeletingAMachineFromAnotherTenantIsNotFound(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-desk-02")
	machine.AwaitOnline()

	outsider := product.TechnicianIn(product.arrangeSeparateTenant("Northwind"))
	assert.Equal(t, http.StatusNotFound,
		outsider.Delete("/api/v1/devices/"+machine.DeviceID.String()).Status)

	require.Len(t, admin.devices(), 1, "somebody else's delete leaves the machine where it was")
}

// assertToldToRemoveItself brings an erased machine back with the certificate it kept and requires
// the removal order to reach it, and the machine never to reappear.
func (p *Product) assertToldToRemoveItself(erased *Machine, quick bool) {
	p.t.Helper()

	back := p.returnWith(erased.identity, erased.Hostname, quick)
	order := back.awaitMessage(p.t, protocol.MsgAgentDeregistered)
	assert.Equal(p.t, "device deleted by administrator", order.Reason)
	_ = back.conn.CloseWithError(0, "removing myself")
	require.Never(p.t, func() bool {
		_, err := p.deviceRow(erased.DeviceID)
		return err == nil
	}, 2*time.Second, poll, "an erased machine must not put itself back by reconnecting")
}

func TestAMachineDeletedWhileOffIsToldToRemoveItselfWhenItReturns(t *testing.T) {
	t.Parallel()

	for name, quick := range map[string]bool{"full reconnect": false, "quick reconnect": true} {
		t.Run(name, func(t *testing.T) {
			product := newProduct(t, WithNumericTelemetry())
			contoso := product.arrangeCustomer("Contoso")
			admin := product.Administrator(contoso)
			machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-vm")
			machine.AwaitOnline()
			machine.Disconnect()

			require.Equal(t, http.StatusNoContent,
				admin.Delete("/api/v1/devices/"+machine.DeviceID.String()).Status)

			product.assertToldToRemoveItself(machine, quick)
		})
	}
}

func TestAMachineDeletedLongAgoIsStillToldToRemoveItself(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-old-laptop")
	machine.AwaitOnline()
	machine.Disconnect()
	require.Equal(t, http.StatusNoContent,
		admin.Delete("/api/v1/devices/"+machine.DeviceID.String()).Status)

	// Its certificate expired more than the 90 days the product admits an expired one to renew.
	product.Advance(90*24*time.Hour + 91*24*time.Hour)

	product.assertToldToRemoveItself(machine, false)
}
