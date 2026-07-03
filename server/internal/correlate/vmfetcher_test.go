package correlate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

type stubExporter struct {
	series      []telemetry.ExportedSeries
	err         error
	gotOrg      uuid.UUID
	gotSelector string
}

func (s *stubExporter) Export(_ context.Context, orgID uuid.UUID, selector string, _, _ time.Time) ([]telemetry.ExportedSeries, error) {
	s.gotOrg = orgID
	s.gotSelector = selector
	return s.series, s.err
}

func TestVMFetcherScopesByDeviceAndMapsPoints(t *testing.T) {
	t.Parallel()
	org := uuid.New()
	dev := uuid.New()
	tsMs := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC).UnixMilli()
	stub := &stubExporter{series: []telemetry.ExportedSeries{
		{
			Metric:     map[string]string{"__name__": "cpu_pct", "org_id": org.String(), "device_id": dev.String(), "core": "0"},
			Values:     []float64{10, 20},
			Timestamps: []int64{tsMs, tsMs + 60000},
		},
	}}
	f := NewVMFetcher(stub)

	got, err := f.Fetch(context.Background(), org, dev, time.UnixMilli(tsMs), time.UnixMilli(tsMs+120000))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// The fetcher must pass a device-scoped selector without org_id; the VM client
	// injects the org matcher itself.
	if stub.gotOrg != org {
		t.Errorf("exporter org = %v, want %v", stub.gotOrg, org)
	}
	wantSelector := `{device_id="` + dev.String() + `"}`
	if stub.gotSelector != wantSelector {
		t.Errorf("selector = %q, want %q", stub.gotSelector, wantSelector)
	}
	if len(got) != 1 {
		t.Fatalf("got %d series, want 1", len(got))
	}
	if got[0].Metric != "cpu_pct" {
		t.Errorf("metric = %q, want cpu_pct", got[0].Metric)
	}
	// Reserved scoping labels must be stripped from the identifying labels.
	if _, ok := got[0].Labels["org_id"]; ok {
		t.Error("org_id label leaked into dimension labels")
	}
	if _, ok := got[0].Labels["device_id"]; ok {
		t.Error("device_id label leaked into dimension labels")
	}
	if got[0].Labels["core"] != "0" {
		t.Errorf("core label = %q, want 0", got[0].Labels["core"])
	}
	if len(got[0].Points) != 2 || got[0].Points[1].Value != 20 {
		t.Fatalf("points = %+v, want two points ending at value 20", got[0].Points)
	}
	if !got[0].Points[0].TS.Equal(time.UnixMilli(tsMs)) {
		t.Errorf("first point TS = %v, want %v", got[0].Points[0].TS, time.UnixMilli(tsMs))
	}
}

func TestVMFetcherPropagatesExportError(t *testing.T) {
	t.Parallel()
	stub := &stubExporter{err: errors.New("vm down")}
	f := NewVMFetcher(stub)
	if _, err := f.Fetch(context.Background(), uuid.New(), uuid.New(), time.Now(), time.Now()); err == nil {
		t.Fatal("expected error to propagate from exporter")
	}
}

func TestVMFetcherSkipsMismatchedValueTimestampLengths(t *testing.T) {
	t.Parallel()
	stub := &stubExporter{series: []telemetry.ExportedSeries{
		{
			Metric:     map[string]string{"__name__": "bad"},
			Values:     []float64{1, 2, 3},
			Timestamps: []int64{100}, // mismatched lengths
		},
	}}
	f := NewVMFetcher(stub)
	got, err := f.Fetch(context.Background(), uuid.New(), uuid.New(), time.Now(), time.Now())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("mismatched series should be skipped, got %+v", got)
	}
}
