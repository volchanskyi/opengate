package acceptance

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/app"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
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

// sweepingOften runs the orphan sweep many times a second and leaves every other worker idle.
var sweepingOften = app.BackgroundSchedule{
	Gauges:           time.Hour,
	DBSize:           time.Hour,
	Investigations:   time.Hour,
	Reconcile:        50 * time.Millisecond,
	PurgeRetry:       time.Hour,
	SessionSweep:     time.Hour,
	SessionGrace:     time.Hour,
	IncidentSweep:    time.Hour,
	PresenceSweep:    time.Hour,
	RetentionSweep:   time.Hour,
	RetentionHorizon: 365 * 24 * time.Hour,
}

func TestAnotherEnvironmentsMachinesKeepTheirReadingsThroughThisServersSweep(t *testing.T) {
	t.Parallel()

	// Both installations run the one default tenant and write to one store, as production and
	// staging do; the suffix keeps other tests' installations out of their sweeps.
	suffix := uuid.NewString()[:8]
	production := newProduct(t, WithNumericTelemetry(), WithSweeps(sweepingOften),
		WithEnvironment("opengate-"+suffix))
	staging := newProduct(t, WithNumericTelemetry(), WithSweeps(sweepingOften),
		WithEnvironment("opengate-staging-"+suffix))

	reportedAt := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	from, to := reportedAt.Add(-5*time.Minute), reportedAt.Add(5*time.Minute)
	type installation struct {
		product *Product
		admin   *Technician
		machine *Machine
	}
	var both []installation
	for i, product := range []*Product{production, staging} {
		admin := product.Administrator(product.arrangeCustomer("Contoso"))
		machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token,
			[]string{"contoso-prod-01", "contoso-stage-01"}[i])
		machine.AwaitOnline()
		machine.report(reportedAt, protocol.MetricDim{Name: "cpu.total", Avg: 12.5})
		admin.awaitReading(product, machine.DeviceID, from, to, "cpu.total")
		both = append(both, installation{product, admin, machine})
	}

	// A second of sweeps every 50 ms gives each installation many passes over the shared store.
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(poll) {
		for _, each := range both {
			each.product.publishReadings()
			require.NotEmpty(t, each.admin.readings(each.machine.DeviceID, from, to, "cpu.total").values("cpu.total"),
				"each installation's sweep leaves the other installation's machine its readings")
		}
	}
}

func TestReadingsQueuedWhenAMachineIsErasedNeverReachTheStore(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-desk-07")
	machine.AwaitOnline()

	// The window waits up to a second in the writer's queue, which the erasure lands inside.
	minute := time.Now().UTC().Truncate(time.Minute).Add(-time.Minute)
	machine.report(minute, protocol.MetricDim{Name: "cpu.total", Avg: 52})
	require.Equal(t, http.StatusNoContent, admin.Delete("/api/v1/devices/"+machine.DeviceID.String()).Status)

	from, to := minute.Add(-5*time.Minute), minute.Add(5*time.Minute)
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		product.publishReadings()
		assert.Zero(t, product.seriesHeld(dbtx.DefaultTenantID, machine.DeviceID, from, to),
			"nothing the machine sent before its erasure is in the store after it")
	}
}
