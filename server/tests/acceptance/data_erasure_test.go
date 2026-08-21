package acceptance

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// TestDeletingAMachineRemovesItAndStopsTrustingItsAgent is the sentence Data
// Erasure promises, in the case that actually happens: the machine is still
// connected when somebody deletes it. The row has to go, the readings have to
// go with it, and the agent's next attempt to connect has to be refused —
// otherwise the machine simply re-registers itself and the erasure undoes
// itself minutes later.
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

	// The agent is still holding a valid certificate. Coming back with it must
	// not put the machine back.
	product.assertAgentIsNoLongerTrusted(machine, token)
}

// TestDeletingAMachineFromAnotherTenantIsNotFound keeps erasure inside its
// tenant. This is the one command that cannot be undone, so the boundary
// matters more here than anywhere.
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

// assertAgentIsNoLongerTrusted has an erased machine's agent try to come back
// with the identity it still holds. Whether the refusal happens at the
// handshake or later, the machine must not reappear — otherwise an erasure
// undoes itself the moment the endpoint reconnects.
func (p *Product) assertAgentIsNoLongerTrusted(erased *Machine, enrolmentToken string) {
	p.t.Helper()

	if err := p.tryReconnect(erased, enrolmentToken); err != nil {
		p.t.Logf("the erased machine's agent was refused at the door: %v", err)
	}
	require.Never(p.t, func() bool {
		_, err := p.deviceRow(erased.DeviceID)
		return err == nil
	}, 2*time.Second, poll, "an erased machine must not put itself back by reconnecting")
}
