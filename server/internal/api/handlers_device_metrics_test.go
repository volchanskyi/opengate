package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// fakeMetricsReader records how the handler invoked the scoped VM reader and
// returns canned series keyed by aggregation.
type fakeMetricsReader struct {
	rangeTenant   uuid.UUID
	rangeCalls    []telemetry.RangeQuery
	rangeByAgg    map[telemetry.RangeAgg][]telemetry.RangeSeries
	rangeErr      error
	instantTenant uuid.UUID
	instant       []telemetry.InstantValue
	instantErr    error
	instantSeen   int
	bands         telemetry.BandCounts
	bandsTenant   uuid.UUID
	bandsWatch    float64
	bandsAnom     float64
	bandsErr      error
	bandsSeen     int
}

func (f *fakeMetricsReader) QueryRange(_ context.Context, tenantID uuid.UUID, rq telemetry.RangeQuery) ([]telemetry.RangeSeries, error) {
	f.rangeTenant = tenantID
	f.rangeCalls = append(f.rangeCalls, rq)
	if f.rangeErr != nil {
		return nil, f.rangeErr
	}
	return f.rangeByAgg[rq.Agg], nil
}

func (f *fakeMetricsReader) QueryInstant(_ context.Context, tenantID uuid.UUID, _ string, _ map[string]string, _ time.Time) ([]telemetry.InstantValue, error) {
	f.instantTenant = tenantID
	f.instantSeen++
	if f.instantErr != nil {
		return nil, f.instantErr
	}
	return f.instant, nil
}

// QueryInstantLookback mirrors QueryInstant for the fake: the badge path uses the
// lookback variant, so it shares the same recorded state and canned result.
func (f *fakeMetricsReader) QueryInstantLookback(_ context.Context, tenantID uuid.UUID, _ string, _ map[string]string, _ time.Time, _ time.Duration) ([]telemetry.InstantValue, error) {
	f.instantTenant = tenantID
	f.instantSeen++
	if f.instantErr != nil {
		return nil, f.instantErr
	}
	return f.instant, nil
}

// CountAnomalyBands records the thresholds the summary handler passed and
// returns the canned band rollup.
func (f *fakeMetricsReader) CountAnomalyBands(_ context.Context, tenantID uuid.UUID, watch, anomalous float64, _ time.Time, _ time.Duration) (telemetry.BandCounts, error) {
	f.bandsTenant = tenantID
	f.bandsWatch = watch
	f.bandsAnom = anomalous
	f.bandsSeen++
	if f.bandsErr != nil {
		return telemetry.BandCounts{}, f.bandsErr
	}
	return f.bands, nil
}

func seedOwnedDevice(t *testing.T, srv *Server) *device.Device {
	t.Helper()
	ctx := testTenantContext(t)
	site := &device.Site{ID: uuid.New(), Name: "metrics-site"}
	require.NoError(t, srv.sites.Create(ctx, site))
	dev := &device.Device{ID: uuid.New(), SiteID: site.ID, Hostname: "metrics-host", OS: "linux", Status: db.StatusOnline}
	require.NoError(t, srv.devices.Upsert(ctx, dev))
	return dev
}

func TestGetDeviceMetricsHandler(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "metrics@example.com", false)
	dev := seedOwnedDevice(t, srv)
	path := "/api/v1/devices/" + dev.ID.String() + "/metrics?from=2026-07-02T00:00:00Z&to=2026-07-02T01:00:00Z"

	// The handler's own grid for that window: one hour at the 10 s raw cadence
	// under the default 1000-point cap, so 360 buckets. The fake answers on it,
	// the way a real store answers the query the grid is issued on.
	handlerFrom := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	handlerGrid := buildMetricGrid(handlerFrom, handlerFrom.Add(time.Hour), chooseStep(handlerFrom, handlerFrom.Add(time.Hour), defaultMaxPoints))
	require.Len(t, handlerGrid.ts, 360)

	t.Run("503 when telemetry not configured", func(t *testing.T) {
		srv.telemetryReader = nil
		w := doRequest(srv, http.MethodGet, path, token, nil)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("400 when window is inverted", func(t *testing.T) {
		srv.telemetryReader = &fakeMetricsReader{}
		bad := "/api/v1/devices/" + dev.ID.String() + "/metrics?from=2026-07-02T01:00:00Z&to=2026-07-02T00:00:00Z"
		w := doRequest(srv, http.MethodGet, bad, token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("200 for another member of the same tenant", func(t *testing.T) {
		_, peerToken := seedTestUser(t, srv, cfg, "other-metrics@example.com", false)
		srv.telemetryReader = &fakeMetricsReader{}
		w := doRequest(srv, http.MethodGet, path, peerToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("200 maps avg + avg_of_10s band, scopes to device, defaults band on", func(t *testing.T) {
		answered := []int64{handlerGrid.ts[0], handlerGrid.ts[6]}
		fake := &fakeMetricsReader{rangeByAgg: map[telemetry.RangeAgg][]telemetry.RangeSeries{
			telemetry.RangeAvg: {{Labels: map[string]string{"dim": "cpu.util"}, Timestamps: answered, Values: []float64{10, 20}}},
			telemetry.RangeMin: {{Labels: map[string]string{"dim": "cpu.util"}, Timestamps: answered, Values: []float64{5, 15}}},
			telemetry.RangeMax: {{Labels: map[string]string{"dim": "cpu.util"}, Timestamps: answered, Values: []float64{15, 25}}},
		}}
		srv.telemetryReader = fake
		w := doRequest(srv, http.MethodGet, path, token, nil)
		require.Equal(t, http.StatusOK, w.Code)

		var resp MetricRangeResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		// The axis is the requested hour, not the two buckets that carried data.
		assert.Equal(t, handlerGrid.ts, resp.T)
		assert.Equal(t, minRangeStepSecs, resp.BucketS)
		require.Len(t, resp.Series, 1)
		assert.Equal(t, "cpu.util", resp.Series[0].Name)
		assert.Equal(t, MetricSeriesMinMaxSourceAvgOf10s, resp.Series[0].MinMaxSource)
		require.NotNil(t, resp.Series[0].Min)
		require.NotNil(t, resp.Series[0].Max)
		require.Len(t, resp.Series[0].Avg, 360)
		assert.Equal(t, []int{0, 6}, nonNullSlots(resp.Series[0].Avg))
		assert.InDelta(t, 20.0, *resp.Series[0].Avg[6], 1e-9)

		// avg + min + max = 3 scoped queries, all filtered to the path device only
		// and all issued on the grid's own edges.
		require.Len(t, fake.rangeCalls, 3)
		for _, c := range fake.rangeCalls {
			assert.Equal(t, dev.ID.String(), c.Matchers["device_id"])
			_, hasTenant := c.Matchers["tenant_id"]
			assert.False(t, hasTenant, "handler must never inject tenant_id itself")
			assert.Equal(t, handlerGrid.ts[0], c.Start.Unix())
			assert.Equal(t, handlerGrid.ts[359], c.End.Unix())
		}
	})

	t.Run("band=none returns avg line only, one query", func(t *testing.T) {
		fake := &fakeMetricsReader{rangeByAgg: map[telemetry.RangeAgg][]telemetry.RangeSeries{
			telemetry.RangeAvg: {{Labels: map[string]string{"dim": "cpu.util"}, Timestamps: []int64{handlerGrid.ts[0]}, Values: []float64{10}}},
		}}
		srv.telemetryReader = fake
		w := doRequest(srv, http.MethodGet, path+"&band=none", token, nil)
		require.Equal(t, http.StatusOK, w.Code)
		var resp MetricRangeResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		require.Len(t, resp.Series, 1)
		assert.Equal(t, MetricSeriesMinMaxSourceNone, resp.Series[0].MinMaxSource)
		assert.Nil(t, resp.Series[0].Min)
		assert.Len(t, fake.rangeCalls, 1)
	})

	t.Run("503 when the range query fails", func(t *testing.T) {
		srv.telemetryReader = &fakeMetricsReader{rangeErr: assertAnErr}
		w := doRequest(srv, http.MethodGet, path, token, nil)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

var assertAnErr = &stubErr{}

type stubErr struct{}

func (*stubErr) Error() string { return "boom" }

func TestChooseStep(t *testing.T) {
	t.Parallel()
	base := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name      string
		window    time.Duration
		maxPoints int
		wantSecs  int64
	}{
		{"one hour into 1000 points floors at 10s", time.Hour, 1000, 10},
		{"seven days into 1000 points widens the bucket", 7 * 24 * time.Hour, 1000, 605},
		{"tiny window still floors at raw cadence", time.Minute, 1000, 10},
		{"exact division does not add an extra bucket second", 1000 * time.Second, 100, 10},
		{"zero window falls back to raw cadence", 0, 1000, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			step := chooseStep(base, base.Add(tt.window), tt.maxPoints)
			assert.Equal(t, tt.wantSecs, int64(step.Seconds()))
			if tt.window > 0 {
				points := int64(tt.window.Seconds()) / int64(step.Seconds())
				assert.LessOrEqual(t, points, int64(tt.maxPoints), "step must bound the point count")
			}
		})
	}
}

func TestClampMaxPoints(t *testing.T) {
	t.Parallel()
	assert.Equal(t, defaultMaxPoints, clampMaxPoints(nil))
	assert.Equal(t, minMaxPointsBound, clampMaxPoints(intPtr(1)))
	assert.Equal(t, maxMaxPointsBound, clampMaxPoints(intPtr(999_999)))
	assert.Equal(t, 750, clampMaxPoints(intPtr(750)))
}

func intPtr(v int) *int { return &v }

func TestAssembleMetricRangeAlignsGridAndGaps(t *testing.T) {
	t.Parallel()
	// mem.used is missing the middle bucket → that slot must be null, and series
	// come back sorted by name regardless of input order.
	grid := buildMetricGrid(time.Unix(100, 0), time.Unix(130, 0), 10*time.Second)
	avg := []telemetry.RangeSeries{
		{Labels: map[string]string{"dim": "mem.used"}, Timestamps: []int64{100, 120}, Values: []float64{1, 3}},
		{Labels: map[string]string{"dim": "cpu.util"}, Timestamps: []int64{100, 110, 120}, Values: []float64{10, 20, 30}},
	}
	got, off := assembleMetricRange(avg, nil, nil, nil, false, grid)

	assert.Zero(t, off.count)
	assert.Equal(t, []int64{100, 110, 120}, got.T)
	assert.Equal(t, 10, got.BucketS)
	assert.False(t, got.Downsampled)
	require.Len(t, got.Series, 2)
	assert.Equal(t, "cpu.util", got.Series[0].Name)
	assert.Equal(t, "mem.used", got.Series[1].Name)

	mem := got.Series[1].Avg
	require.Len(t, mem, 3)
	require.NotNil(t, mem[0])
	assert.Nil(t, mem[1], "missing middle bucket must be null")
	require.NotNil(t, mem[2])
}

func TestAssembleMetricRangeDimFilterAndDownsampled(t *testing.T) {
	t.Parallel()
	grid := buildMetricGrid(time.Unix(60, 0), time.Unix(120, 0), 60*time.Second)
	avg := []telemetry.RangeSeries{
		{Labels: map[string]string{"dim": "cpu.util"}, Timestamps: []int64{60}, Values: []float64{10}},
		{Labels: map[string]string{"dim": "mem.used"}, Timestamps: []int64{60}, Values: []float64{5}},
	}
	got, off := assembleMetricRange(avg, nil, nil, map[string]bool{"cpu.util": true}, false, grid)
	assert.Zero(t, off.count)
	require.Len(t, got.Series, 1)
	assert.Equal(t, "cpu.util", got.Series[0].Name)
	assert.True(t, got.Downsampled, "60s bucket is coarser than the 10s raw cadence")
}

func TestEnrichAnomalyRates(t *testing.T) {
	srv, _ := newTestServer(t)
	dev := seedOwnedDevice(t, srv)
	other := uuid.New()

	fake := &fakeMetricsReader{instant: []telemetry.InstantValue{
		{Labels: map[string]string{"device_id": dev.ID.String()}, Value: 0.42},
		{Labels: map[string]string{"device_id": other.String()}, Value: 0.99},
	}}
	srv.telemetryReader = fake

	devices := []Device{{Id: dev.ID}, {Id: uuid.New()}}
	srv.enrichAnomalyRates(testTenantContext(t), devices)

	require.NotNil(t, devices[0].AnomalyRate)
	assert.InDelta(t, 0.42, float64(*devices[0].AnomalyRate), 1e-6)
	assert.Nil(t, devices[1].AnomalyRate, "device without a sample stays unset")
	assert.Equal(t, 1, fake.instantSeen)
}

func TestEnrichAnomalyRatesBestEffort(t *testing.T) {
	srv, _ := newTestServer(t)
	// nil reader and query error must both leave devices untouched, never panic.
	srv.telemetryReader = nil
	devices := []Device{{Id: uuid.New()}}
	srv.enrichAnomalyRates(testTenantContext(t), devices)
	assert.Nil(t, devices[0].AnomalyRate)

	srv.telemetryReader = &fakeMetricsReader{instantErr: assertAnErr}
	srv.enrichAnomalyRates(testTenantContext(t), devices)
	assert.Nil(t, devices[0].AnomalyRate)
}
