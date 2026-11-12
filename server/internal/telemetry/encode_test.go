package telemetry

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hourOfReadings(start time.Time) []Sample {
	labels := map[string]string{"dim": "cpu.total"}
	samples := make([]Sample, 60)
	for i := range samples {
		samples[i] = Sample{Name: "opengate_edge_metric_avg", Value: float64(i) / 3,
			TS: start.Add(time.Duration(i) * time.Minute), Labels: labels}
	}
	return samples
}

func TestEncodingAnHourOfReadingsAllocatesTheBodyAndLittleElse(t *testing.T) {
	scope := Scope{Tenant: uuid.New(), Device: uuid.New()}
	samples := hourOfReadings(time.Unix(1_800_000_000, 0).UTC())

	allocs := testing.AllocsPerRun(50, func() {
		if Encode(scope, samples).Size() == 0 {
			t.Fatal("an hour of valid readings must encode")
		}
	})

	assert.LessOrEqual(t, allocs, 3.0, "allocations per batch of %d readings", len(samples))
}

func TestEncodedBodyHoldsEachReadingOnItsOwnLineInTheStoreFormat(t *testing.T) {
	tenant := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	device := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	at := time.Unix(1_700_000_000, 0).UTC()
	samples := []Sample{
		{Name: "m", Value: 0.1, TS: at, Labels: map[string]string{"zone": `x\y`, "dim": `a"b`}},
		{Name: "m", Value: 1e21, TS: at.Add(time.Second)},
		{Name: "bad name", Value: 1, TS: at},
		{Name: "m:sub", Value: -3, TS: at.Add(2 * time.Second), Labels: map[string]string{"a": "line\nbreak"}},
	}

	encoded := Encode(Scope{Tenant: tenant, Device: device}, samples)

	ids := `device_id="33333333-3333-3333-3333-333333333333"`
	tenantLabel := `tenant_id="22222222-2222-2222-2222-222222222222"`
	want := strings.Join([]string{
		`m{` + ids + `,dim="a\"b",` + tenantLabel + `,zone="x\\y"} 0.1 1700000000000`,
		`m{` + ids + `,` + tenantLabel + `} 1e+21 1700000001000`,
		`m:sub{a="line\nbreak",` + ids + `,` + tenantLabel + `} -3 1700000002000`,
	}, "\n") + "\n"
	assert.Equal(t, want, string(encoded.readings.body))
	assert.Equal(t, 3, encoded.readings.kept)
	assert.Equal(t, 1, encoded.readings.refused)
	assert.Equal(t, at, encoded.readings.first)
}

func TestEncodingAWindowWithNoReadingTheStoreAcceptsKeepsNothing(t *testing.T) {
	encoded := Encode(Scope{Tenant: uuid.New(), Device: uuid.New()}, []Sample{{Name: "1bad", Value: 1}})

	require.Zero(t, encoded.Size())
	assert.Equal(t, 1, encoded.readings.refused)
}
