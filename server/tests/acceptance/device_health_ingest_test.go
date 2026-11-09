package acceptance

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// catchUpMachine connects a machine that can send its history after an outage.
func catchUpMachine(product *Product, admin *Technician, hostname string) *Machine {
	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, hostname,
		protocol.CapTerminal, protocol.CapBackfill)
	machine.AwaitOnline()
	return machine
}

// catchUpBatch is one batch of a machine's own history, ending at its last reading.
func catchUpBatch(samples ...protocol.BackfillSample) *protocol.ControlMessage {
	return &protocol.ControlMessage{
		Type:            protocol.MsgMetricBackfillBatch,
		Tier:            protocol.BackfillTierRecent60s,
		BackfillSamples: samples,
		Cursor:          samples[len(samples)-1].TS,
	}
}

func TestAMachineBackFromAnOutageFillsItsChartWhereTheReadingsHappened(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	machine := catchUpMachine(product, admin, "contoso-laptop-11")

	machine.Send(&protocol.ControlMessage{Type: protocol.MsgRequestBackfillSlot, PendingSamples: 2})
	machine.Await(protocol.MsgGrantBackfill)

	threeHoursAgo := time.Now().UTC().Truncate(time.Minute).Add(-3 * time.Hour)
	twoHoursAgo := threeHoursAgo.Add(time.Hour)
	machine.Send(catchUpBatch(
		protocol.BackfillSample{Name: "cpu.total", TS: threeHoursAgo.Unix(), Value: 21.5},
		protocol.BackfillSample{Name: "cpu.total", TS: twoHoursAgo.Unix(), Value: 33.25},
	))

	answer := machine.Await(protocol.MsgMetricBackfillAck)
	assert.Equal(t, twoHoursAgo.Unix(), answer.Cursor, "the answer names the batch it confirms")

	for _, reading := range []struct {
		at    time.Time
		value float64
	}{{threeHoursAgo, 21.5}, {twoHoursAgo, 33.25}} {
		from, to := reading.at.Add(-5*time.Minute), reading.at.Add(5*time.Minute)
		admin.awaitReading(product, machine.DeviceID, from, to, "cpu.total")
		assert.Equal(t, []float64{reading.value}, admin.readings(machine.DeviceID, from, to, "cpu.total").values("cpu.total"),
			"the reading sits at the minute it was measured, not the minute it arrived")
	}
}

func TestCatchUpSentWithoutPermissionIsToldToWaitAndStoresNothing(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	machine := catchUpMachine(product, admin, "contoso-laptop-12")

	anHourAgo := time.Now().UTC().Truncate(time.Minute).Add(-time.Hour)
	machine.Send(catchUpBatch(protocol.BackfillSample{Name: "cpu.total", TS: anHourAgo.Unix(), Value: 64}))

	wait := machine.Await(protocol.MsgDeferBackfill)
	assert.Positive(t, wait.RetryAfter, "the machine is told when to ask again")
	assert.False(t, machine.Received(protocol.MsgMetricBackfillAck), "the batch is not confirmed")

	product.publishReadings()
	from, to := anHourAgo.Add(-5*time.Minute), anHourAgo.Add(5*time.Minute)
	assert.Empty(t, admin.readings(machine.DeviceID, from, to, "cpu.total").values("cpu.total"),
		"nothing sent without permission reaches the chart")
}

func TestOneBadReadingCostsNothingElseInItsMinute(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-server-04")
	machine.AwaitOnline()

	reportedAt := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	machine.report(reportedAt,
		protocol.MetricDim{Name: "cpu.total", Avg: math.NaN()},
		protocol.MetricDim{Name: "mem.used_percent", Avg: 44.25},
	)
	machine.settle()

	from, to := reportedAt.Add(-5*time.Minute), reportedAt.Add(5*time.Minute)
	admin.awaitReading(product, machine.DeviceID, from, to, "mem.used_percent")
	chart := admin.readings(machine.DeviceID, from, to, "cpu.total", "mem.used_percent")
	assert.Equal(t, []float64{44.25}, chart.values("mem.used_percent"), "the good reading of the minute is kept")
	assert.Empty(t, chart.values("cpu.total"), "the reading that is not a number is not")
	assert.Equal(t, 1.0, admin.dropsCounted("invalid_value"), "and its refusal is counted under its reason")
}

func TestHistoryOlderThanTheServerKeepsIsAnsweredAndNotStored(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	machine := catchUpMachine(product, admin, "contoso-laptop-13")

	machine.Send(&protocol.ControlMessage{Type: protocol.MsgRequestBackfillSlot, PendingSamples: 1})
	machine.Await(protocol.MsgGrantBackfill)

	monthAgo := time.Now().UTC().Truncate(time.Minute).Add(-31 * 24 * time.Hour)
	machine.Send(catchUpBatch(protocol.BackfillSample{Name: "cpu.total", TS: monthAgo.Unix(), Value: 12}))

	answer := machine.Await(protocol.MsgMetricBackfillAck)
	assert.Equal(t, monthAgo.Unix(), answer.Cursor, "the machine is told to move past it")
	assert.Equal(t, 1.0, admin.dropsCounted("backfill_out_of_retention"),
		"history past the server's thirty days is counted, never written")
}

func TestBothKindsOfHealthSummaryInOneBreathAreKept(t *testing.T) {
	t.Parallel()

	product := newProduct(t, WithNumericTelemetry())
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)

	machine := product.Machine(admin.mintEnrolmentToken("Head Office").Token, "contoso-server-05")
	machine.AwaitOnline()

	// A firing rule's breach summary lands a second before the machine's minute anomaly summary,
	// both stamped two minutes back so the store already shows them to a query.
	sentAt := time.Now().UTC().Add(-2 * time.Minute).Unix()
	machine.Send(&protocol.ControlMessage{
		Type: protocol.MsgAgentHealthSummary, TS: sentAt - 1,
		Breaches: []protocol.AlertBreach{{RuleID: "cpu-high", Metric: "cpu.total", Value: 97}},
	})
	machine.Send(&protocol.ControlMessage{
		Type: protocol.MsgAgentHealthSummary, TS: sentAt,
		SamplerVersion: "s1", ModelVersion: "m1", NodeAnomalyRate: 0.375,
	})
	machine.settle()

	require.Eventuallyf(t, func() bool {
		product.publishReadings()
		for _, listed := range admin.devices() {
			if listed.ID == machine.DeviceID && listed.AnomalyRate != nil {
				return *listed.AnomalyRate == 0.375
			}
		}
		return false
	}, eventually, 200*time.Millisecond, "the anomaly rate sent a second after a breach reaches the device list")
	assert.Equal(t, 0.0, admin.dropsCounted("interval_floor"), "neither summary is refused")
}

// platformInstrumentation returns the metrics exposition served by the cluster-only listener.
// dropsCounted reads how many telemetry messages the platform has dropped under reason.
func (a *Technician) dropsCounted(reason string) float64 {
	a.t.Helper()
	prefix := fmt.Sprintf(`opengate_edge_telemetry_drops_total{reason=%q} `, reason)
	for _, line := range strings.Split(a.platformInstrumentation(), "\n") {
		if value, ok := strings.CutPrefix(line, prefix); ok {
			count, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			require.NoError(a.t, err)
			return count
		}
	}
	require.Failf(a.t, "drop reason not published", "the platform publishes no %s drops", reason)
	return 0
}
