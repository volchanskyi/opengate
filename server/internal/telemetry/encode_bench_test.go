package telemetry

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// benchVitals is the dimension list a live window carries, one reading each.
var benchVitals = []string{
	"cpu.total", "cpu.total.max", "mem.used_percent", "mem.used_percent.max", "disk.used_percent",
	"net.rx_bps", "net.rx_bps.max", "net.tx_bps", "net.tx_bps.max", "disk.mounts_critical",
	"stall.cpu.some", "stall.mem.some", "stall.mem.full", "stall.io.some", "stall.io.full",
	"disk.await_ms", "disk.await_ms.max", "disk.queue_depth",
}

func benchScope() Scope { return Scope{Tenant: uuid.New(), Device: uuid.New()} }

func BenchmarkEncodeLiveWindow(b *testing.B) {
	scope := benchScope()
	at := time.Unix(1_800_000_000, 0).UTC()
	samples := make([]Sample, len(benchVitals))
	for i, dim := range benchVitals {
		samples[i] = Sample{Name: "opengate_edge_metric_avg", Value: float64(i) + 0.25, TS: at,
			Labels: map[string]string{"dim": dim}}
	}

	b.ReportAllocs()
	for b.Loop() {
		if Encode(scope, samples).Size() == 0 {
			b.Fatal("a live window of valid readings must encode")
		}
	}
}

func BenchmarkEncodeCatchUpBatch(b *testing.B) {
	scope := benchScope()
	start := time.Unix(1_800_000_000, 0).UTC()
	samples := make([]Sample, 60)
	for i := range samples {
		samples[i] = Sample{Name: "opengate_edge_metric_avg", Value: float64(i) / 3,
			TS: start.Add(time.Duration(i) * time.Minute), Labels: map[string]string{"dim": "cpu.total"}}
	}

	b.ReportAllocs()
	for b.Loop() {
		if Encode(scope, samples).Size() == 0 {
			b.Fatal("an hour of valid readings must encode")
		}
	}
}
