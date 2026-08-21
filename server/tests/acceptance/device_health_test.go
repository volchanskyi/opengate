package acceptance

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// readings is the chart a technician sees on a machine's page: an axis of
// bucket timestamps and one line per dimension, aligned to it.
type readings struct {
	T      []int64 `json:"t"`
	Series []struct {
		Name string     `json:"name"`
		Avg  []*float64 `json:"avg"`
	} `json:"series"`
}

// values returns the readings a technician can actually see on one line — the
// buckets the machine reported, with the empty ones dropped the way an eye
// drops them.
func (r readings) values(dim string) []float64 {
	var out []float64
	for _, series := range r.Series {
		if series.Name != dim {
			continue
		}
		for _, v := range series.Avg {
			if v != nil {
				out = append(out, *v)
			}
		}
	}
	return out
}

// readings asks the device page for a machine's numbers over a window.
func (a *Technician) readings(deviceID fmt.Stringer, from, to time.Time, dims ...string) readings {
	a.t.Helper()

	path := fmt.Sprintf("/api/v1/devices/%s/metrics?from=%s&to=%s&dims=%s",
		deviceID, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339), strings.Join(dims, ","))

	reply := a.Get(path)
	require.Equalf(a.t, http.StatusOK, reply.Status, "reading the chart failed: %s", reply.Text())

	var out readings
	reply.Into(&out)
	return out
}

// report is one minute of readings, the way a machine closes a window and
// sends what it saw.
func (m *Machine) report(at time.Time, dims ...protocol.MetricDim) {
	m.t.Helper()
	m.Send(&protocol.ControlMessage{Type: protocol.MsgAgentMetricWindow, TS: at.Unix(), Dims: dims})
}

// settle sends a heartbeat, which is the boundary the machine's telemetry
// burst is written at. A machine that has reported and then gone quiet has
// already sent one; a test that reports and then reads must too, or it reads
// before the write.
func (m *Machine) settle() {
	m.t.Helper()
	m.Send(&protocol.ControlMessage{Type: protocol.MsgAgentHeartbeat, Timestamp: time.Now().UTC().Unix()})
}

// TestAMachineReportsAMinuteAndTheTechnicianReadsItBack is the sentence Device
// Health promises, and it is the one a shipped defect walked straight through.
//
// The write half had coverage against in-process fakes, the read half against
// a fake reader, and the metrics client against a real store — four halves and
// no whole. What that arrangement could not see is a path that counts a
// reading as received and then never writes it: the fleet measured thousands
// of windows ingested, one persisted, and zero dropped, and every tier stayed
// green for the whole of it. This test is the only shape that goes red for
// that, because it asks the question a technician asks — is the number on the
// page?
func TestAMachineReportsAMinuteAndTheTechnicianReadsItBack(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-server-01")
	machine.AwaitOnline()

	reportedAt := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	machine.report(reportedAt,
		protocol.MetricDim{Name: "cpu.total", Avg: 71.5},
		protocol.MetricDim{Name: "mem.used_percent", Avg: 44.25},
	)
	machine.settle()

	from, to := reportedAt.Add(-5*time.Minute), reportedAt.Add(5*time.Minute)
	admin.awaitReading(product, machine.DeviceID, from, to, "cpu.total")

	chart := admin.readings(machine.DeviceID, from, to, "cpu.total", "mem.used_percent")
	assert.Contains(t, chart.values("cpu.total"), 71.5,
		"the value the technician reads is the value the machine reported")
	assert.Contains(t, chart.values("mem.used_percent"), 44.25)
	assert.NotEmpty(t, chart.T, "the chart carries the window's own axis, not just the buckets that had data")
}

// TestReadingsThatArriveWithNothingInThemAreAccountedFor is the empty-payload
// case, named because it is the one that cost the fleet. Every message the
// product receives either lands or says why not; a message that is counted as
// received and then quietly discarded is the failure this states against.
func TestReadingsThatArriveWithNothingInThemAreAccountedFor(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-server-02")
	machine.AwaitOnline()

	machine.report(time.Now().UTC().Add(-time.Minute))
	machine.settle()

	require.Eventually(t, func() bool {
		return strings.Contains(admin.platformInstrumentation(), `reason="empty_dims"`)
	}, eventually, 100*time.Millisecond,
		"a window with nothing in it must be counted as a drop, with the reason said out loud")
}

// TestAReadingFromAMachineWithAWrongClockIsStillKept covers the laptop coming
// back from a suspended virtual machine that stamps its readings hours out. A
// clamp is not a drop: the reading is pulled to the nearer bound of what the
// product will accept, and it is still on the page.
func TestAReadingFromAMachineWithAWrongClockIsStillKept(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-laptop-07")
	machine.AwaitOnline()

	// A clock hours ahead of everybody else's.
	machine.report(time.Now().UTC().Add(6*time.Hour), protocol.MetricDim{Name: "cpu.total", Avg: 12.5})
	machine.settle()

	from, to := time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Hour)
	admin.awaitReading(product, machine.DeviceID, from, to, "cpu.total")
}

// TestADimensionTheFleetNeverAgreedToIsRefused pins the bound on what a
// machine may put in the store. A dimension name arrives as untrusted input,
// and copying it into a label would make the whole tenant's series count a
// property of what one machine sends.
func TestADimensionTheFleetNeverAgreedToIsRefused(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-server-03")
	machine.AwaitOnline()

	machine.report(time.Now().UTC().Add(-time.Minute),
		protocol.MetricDim{Name: "attacker.invented.dim", Avg: 1})
	machine.settle()

	require.Eventually(t, func() bool {
		return strings.Contains(admin.platformInstrumentation(), `reason="unknown_dim"`)
	}, eventually, 100*time.Millisecond,
		"a dimension outside the agreed vocabulary is dropped and counted, never stored")
}

// awaitReading waits until a dimension the machine reported is readable on the
// device page. Each attempt publishes what the store already holds first, so
// the wait is on the product having written the reading rather than on the
// store's batching timer.
func (a *Technician) awaitReading(product *Product, deviceID fmt.Stringer, from, to time.Time, dim string) {
	a.t.Helper()
	require.Eventuallyf(a.t, func() bool {
		product.publishReadings()
		return len(a.readings(deviceID, from, to, dim).values(dim)) > 0
	}, eventually, 200*time.Millisecond,
		"a %s reading the machine sent must become a number on the machine's page", dim)
}

// platformInstrumentation is the platform's own reading of itself — what a
// monitoring system scrapes. It is a door an operator has, which is why an
// outcome about accounting may be stated through it.
func (a *Technician) platformInstrumentation() string {
	a.t.Helper()
	return a.Get("/metrics").Text()
}
