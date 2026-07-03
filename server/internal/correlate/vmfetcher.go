package correlate

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// reservedLabels are server-owned scoping labels that must not appear as
// identifying dimension labels in a correlation result.
var reservedLabels = map[string]bool{"org_id": true, "device_id": true, "__name__": true}

// vmExporter is the subset of telemetry.VMClient the fetcher needs. Keeping it an
// interface lets tests substitute a stub and keeps the VM endpoint strings inside
// the telemetry package (the scoped-read grep gate).
type vmExporter interface {
	Export(ctx context.Context, orgID uuid.UUID, selector string, start, end time.Time) ([]telemetry.ExportedSeries, error)
}

// VMFetcher adapts the tenant-scoped VictoriaMetrics client into a SeriesFetcher.
type VMFetcher struct {
	vm vmExporter
}

// NewVMFetcher wraps a scoped VM exporter (telemetry.VMClient) as a SeriesFetcher.
func NewVMFetcher(vm vmExporter) *VMFetcher {
	return &VMFetcher{vm: vm}
}

// Fetch pulls every numeric series carrying the device_id label over the window.
// The selector deliberately omits org_id; telemetry.VMClient injects the
// authoritative tenant matcher, so one org can never read another's series.
func (f *VMFetcher) Fetch(ctx context.Context, orgID, deviceID uuid.UUID, start, end time.Time) ([]Series, error) {
	selector := fmt.Sprintf(`{device_id=%q}`, deviceID.String())
	exported, err := f.vm.Export(ctx, orgID, selector, start, end)
	if err != nil {
		return nil, err
	}
	out := make([]Series, 0, len(exported))
	for _, es := range exported {
		if len(es.Values) != len(es.Timestamps) || len(es.Values) == 0 {
			continue
		}
		s := Series{Metric: es.Metric["__name__"]}
		if len(es.Metric) > 0 {
			s.Labels = make(map[string]string, len(es.Metric))
			for k, v := range es.Metric {
				if reservedLabels[k] {
					continue
				}
				s.Labels[k] = v
			}
		}
		s.Points = make([]Point, len(es.Values))
		for i := range es.Values {
			s.Points[i] = Point{TS: time.UnixMilli(es.Timestamps[i]), Value: es.Values[i]}
		}
		out = append(out, s)
	}
	return out, nil
}
