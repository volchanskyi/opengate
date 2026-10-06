package acceptance

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// readings is a machine's chart: bucket timestamps and one aligned series per dimension.
type readings struct {
	T      []int64 `json:"t"`
	Series []struct {
		Name string     `json:"name"`
		Avg  []*float64 `json:"avg"`
	} `json:"series"`
}

// values returns one series' reported readings with the empty buckets dropped.
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

// readings fetches a machine's numbers over a window.
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

// report sends one closed minute window of readings.
func (m *Machine) report(at time.Time, dims ...protocol.MetricDim) {
	m.t.Helper()
	m.Send(&protocol.ControlMessage{Type: protocol.MsgAgentMetricWindow, TS: at.Unix(), Dims: dims})
}

// settle sends a heartbeat, the boundary at which the telemetry burst is written.
func (m *Machine) settle() {
	m.t.Helper()
	m.Send(&protocol.ControlMessage{Type: protocol.MsgAgentHeartbeat, Timestamp: time.Now().UTC().Unix()})
}

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

func TestAReadingFromAMachineWithAWrongClockIsStillKept(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-laptop-07")
	machine.AwaitOnline()

	// The timestamp is six hours ahead, beyond the accepted window, so the reading is clamped.
	machine.report(time.Now().UTC().Add(6*time.Hour), protocol.MetricDim{Name: "cpu.total", Avg: 12.5})
	machine.settle()

	from, to := time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Hour)
	admin.awaitReading(product, machine.DeviceID, from, to, "cpu.total")
}

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

// awaitReading waits until a reported dimension is readable, publishing the store's pending
// batch on each attempt.
func (a *Technician) awaitReading(product *Product, deviceID fmt.Stringer, from, to time.Time, dim string) {
	a.t.Helper()
	require.Eventuallyf(a.t, func() bool {
		product.publishReadings()
		return len(a.readings(deviceID, from, to, dim).values(dim)) > 0
	}, eventually, 200*time.Millisecond,
		"a %s reading the machine sent must become a number on the machine's page", dim)
}

// platformInstrumentation returns the metrics exposition served by the cluster-only listener.
func (a *Technician) platformInstrumentation() string {
	a.t.Helper()
	req, err := http.NewRequestWithContext(a.t.Context(),
		http.MethodGet, a.product.Internal.URL+"/metrics", nil)
	require.NoError(a.t, err)
	resp, err := a.product.Internal.Client().Do(req)
	require.NoError(a.t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(a.t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(a.t, err)
	return string(body)
}
