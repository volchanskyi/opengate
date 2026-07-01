// Package vmbackfill is a feasibility measurement proving how historical
// backfill must reach VictoriaMetrics. Reconnecting agents replay pre-rolled
// rollups for past intervals; those must land at their ORIGINAL timestamps.
//
// The import API preserves caller-supplied timestamps (proven here), so it is
// the correct backfill path. Stream aggregation, by contrast, buckets samples
// by ARRIVAL time — replaying a day-old rollup through it would stamp the point
// "now" and silently misplace it on every chart. Hence backfill writes
// pre-rolled rollups via the import API and never through stream aggregation;
// stream aggregation is only for live telemetry, where arrival ≈ event time.
package vmbackfill

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testvm"
)

type exportedSeries struct {
	Metric     map[string]string `json:"metric"`
	Values     []float64         `json:"values"`
	Timestamps []int64           `json:"timestamps"`
}

// TestImportPreservesOriginalTimestamps writes rollup points backdated by
// hours, days, and a week, then asserts VM returns them at exactly those
// instants and values, in order — the property that makes the import API the
// correct backfill path.
func TestImportPreservesOriginalTimestamps(t *testing.T) {
	base := testvm.BaseURL(t)
	now := time.Now()

	// Distinct historical instants a reconnecting agent might replay.
	wantTS := []int64{
		now.Add(-7 * 24 * time.Hour).UnixMilli(),
		now.Add(-48 * time.Hour).UnixMilli(),
		now.Add(-2 * time.Hour).UnixMilli(),
	}
	wantVal := []float64{11, 22, 33}

	var b strings.Builder
	for i, ts := range wantTS {
		fmt.Fprintf(&b, "es_backfill_preserve{org_id=%q} %g %d\n", "org-1", wantVal[i], ts)
	}
	importAndFlush(t, base, b.String())

	got := exportSeries(t, base, "es_backfill_preserve", now.Add(-30*24*time.Hour), now)
	require.Equal(t, wantTS, got.Timestamps, "backfilled samples must keep their original timestamps")
	require.Equal(t, wantVal, got.Values, "values must stay aligned to their original timestamps")
}

// TestBackfillNotArrivalBucketed asserts no backfilled sample lands near ingest
// time — the failure mode stream aggregation would exhibit. Every stored
// timestamp must remain in the historical past it was written for.
func TestBackfillNotArrivalBucketed(t *testing.T) {
	base := testvm.BaseURL(t)
	now := time.Now()
	arrivalFloor := now.Add(-time.Hour).UnixMilli()

	oldest := now.Add(-30 * 24 * time.Hour).UnixMilli()
	mid := now.Add(-10 * 24 * time.Hour).UnixMilli()
	body := fmt.Sprintf("es_backfill_arrival{org_id=%q} 1 %d\nes_backfill_arrival{org_id=%q} 2 %d\n",
		"org-1", oldest, "org-1", mid)
	importAndFlush(t, base, body)

	got := exportSeries(t, base, "es_backfill_arrival", now.Add(-60*24*time.Hour), now)
	require.NotEmpty(t, got.Timestamps)
	for _, ts := range got.Timestamps {
		require.Lessf(t, ts, arrivalFloor,
			"sample at %d collapsed toward arrival time (%d) — the stream-agg bucketing trap", ts, arrivalFloor)
	}
}

// importAndFlush posts Prometheus exposition text via the import API and forces
// a flush so the samples are immediately queryable (deterministic, no polling).
func importAndFlush(t *testing.T, base, body string) {
	t.Helper()
	post(t, base+"/api/v1/import/prometheus", strings.NewReader(body), http.StatusNoContent)
	post(t, base+"/internal/force_flush", nil, http.StatusOK)
}

func post(t *testing.T, target string, body io.Reader, wantStatus int) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, target, body)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, wantStatus, resp.StatusCode, target)
}

// exportSeries reads the single series matching metricName over [start,end] via
// the export API, returning its raw stored values and timestamps.
func exportSeries(t *testing.T, base, metricName string, start, end time.Time) exportedSeries {
	t.Helper()
	q := url.Values{}
	q.Set("match[]", metricName)
	q.Set("start", strconv.FormatInt(start.Unix(), 10))
	q.Set("end", strconv.FormatInt(end.Unix(), 10))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/api/v1/export?"+q.Encode(), nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// The export API streams newline-delimited JSON, one object per series.
	dec := json.NewDecoder(resp.Body)
	for dec.More() {
		var es exportedSeries
		require.NoError(t, dec.Decode(&es))
		if es.Metric["__name__"] == metricName {
			return es
		}
	}
	t.Fatalf("export returned no %q series", metricName)
	return exportedSeries{}
}
