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

	require.Eventually(t, func() bool {
		return admin.dropsCounted("empty_dims") >= 1
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

	require.Eventually(t, func() bool {
		return admin.dropsCounted("unknown_dim") >= 1
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

// livePromise is how soon a live minute reaches the chart during anything else the server does.
const livePromise = 5 * time.Second

func TestALiveMinuteReachesTheChartWithoutWaitingForAHeartbeat(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	admin := product.Administrator(product.arrangeCustomer("Contoso"))
	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-server-04")
	machine.AwaitOnline()

	minute := time.Now().UTC().Truncate(time.Minute).Add(-time.Minute)
	sent := time.Now()
	machine.report(minute, protocol.MetricDim{Name: "cpu.total", Avg: 64})

	from, to := minute.Add(-5*time.Minute), minute.Add(5*time.Minute)
	require.Eventually(t, func() bool {
		product.publishReadings()
		return len(admin.readings(machine.DeviceID, from, to, "cpu.total").values("cpu.total")) > 0
	}, livePromise, 100*time.Millisecond, "a live minute is on the chart within five seconds, with no heartbeat")
	assert.Less(t, time.Since(sent), livePromise)
}

func TestAMachineIsToldEachMinuteItSentWasStored(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	admin := product.Administrator(product.arrangeCustomer("Contoso"))
	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-server-05",
		protocol.CapLiveConfirmations)
	machine.AwaitOnline()

	first := time.Now().UTC().Truncate(time.Minute).Add(-2 * time.Minute)
	for _, minute := range []time.Time{first, first.Add(time.Minute)} {
		machine.report(minute, protocol.MetricDim{Name: "cpu.total", Avg: 40})
	}

	for _, minute := range []time.Time{first, first.Add(time.Minute)} {
		machine.awaitConfirmation(minute)
	}
}

// awaitConfirmation waits for the product to say the minute stamped at was stored.
func (m *Machine) awaitConfirmation(at time.Time) {
	m.t.Helper()
	require.Eventuallyf(m.t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, msg := range m.inbox {
			if msg.Type == protocol.MsgLiveWindowStored && msg.TS == at.Unix() {
				return true
			}
		}
		return false
	}, eventually, poll, "the machine was never told its minute %s was stored", at.Format(time.RFC3339))
}
