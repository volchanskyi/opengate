package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The three technician journeys a run already times, carried into the evidence.
//
// A slow night that can only say "the API was slow" names nothing anybody can
// act on. The technician-side generator times a machine list, a machine's own
// page and a command being accepted, publishes all three into the trend, and
// the bundle beside them carried a null — so a bundle read a year later, when
// the metrics store has long since forgotten the night, says which screens
// existed and not which one was slow.
//
// Nothing is measured twice here. The figures are read out of the export the
// generator already writes.

// journeyPrefix and journeySuffix bracket the names the generator publishes:
// `journey_device_list_ms` is the device-list journey in milliseconds.
const (
	journeyPrefix = "journey_"
	journeySuffix = "_ms"
)

// k6Export is the shape of the summary the technician-side generator writes.
// Only the metrics block is read, the way the exposition readers beside this
// one read only the families they name.
type k6Export struct {
	Metrics map[string]struct {
		Type   string             `json:"type"`
		Values map[string]float64 `json:"values"`
	} `json:"metrics"`
}

// LoadJourneys reads the journeys out of one generator export.
//
// An export carrying none is silence rather than an error: the technician-side
// generator does not run in every venue, and a venue without one has no
// journeys to report rather than a broken reading.
func LoadJourneys(path string) ([]JourneyResult, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read journeys %s: %w", path, err)
	}

	var export k6Export
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("decode journeys %s: %w", path, err)
	}

	var journeys []JourneyResult
	for metric, series := range export.Metrics {
		name, ok := journeyName(metric)
		if !ok {
			continue
		}
		journeys = append(journeys, JourneyResult{
			Name:         name,
			Requests:     int64(series.Values["count"]),
			LatencyP50Ms: series.Values["med"],
			LatencyP95Ms: series.Values["p(95)"],
		})
	}

	// Named order, so two runs of the same night list their journeys the same
	// way and a reader diffing two bundles sees the numbers change rather than
	// the order.
	sort.Slice(journeys, func(i, j int) bool { return journeys[i].Name < journeys[j].Name })
	return journeys, nil
}

// journeyName is the journey a metric describes, or false for a metric that is
// not one. Every other series in the export belongs to the request path rather
// than to a screen.
func journeyName(metric string) (string, bool) {
	if !strings.HasPrefix(metric, journeyPrefix) || !strings.HasSuffix(metric, journeySuffix) {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(metric, journeyPrefix), journeySuffix)
	if name == "" {
		return "", false
	}
	return strings.ReplaceAll(name, "_", "-"), true
}

// FixtureWeight is what the fleet actually cost on disk, measured from the
// database rather than counted from the plan.
type FixtureWeight struct {
	DatabaseBytes   int64 `json:"fixture_bytes"`
	TelemetrySeries int64 `json:"telemetry_series"`
}

// LoadFixtureWeight reads the weighing a run took beside itself.
//
// The volume family's whole finding is this number, and the job that measures
// it wrote the figure into a file of its own that nothing downstream read — so
// the one measurement answering the family's question never reached the
// evidence the family produces.
func LoadFixtureWeight(path string) (FixtureWeight, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return FixtureWeight{}, fmt.Errorf("read fixture weight %s: %w", path, err)
	}
	var weight FixtureWeight
	if err := json.Unmarshal(data, &weight); err != nil {
		return FixtureWeight{}, fmt.Errorf("decode fixture weight %s: %w", path, err)
	}
	return weight, nil
}
