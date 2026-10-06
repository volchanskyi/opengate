package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Published names look like journey_device_list_ms, in milliseconds.
const (
	journeyPrefix = "journey_"
	journeySuffix = "_ms"
)

// k6Export reads only the metrics block of the generator's summary.
type k6Export struct {
	Metrics map[string]struct {
		Type   string             `json:"type"`
		Values map[string]float64 `json:"values"`
	} `json:"metrics"`
}

// LoadJourneys reads the journeys from one generator export; an export carrying none yields none.
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

	// Name order keeps the journey lists of two bundles diffable.
	sort.Slice(journeys, func(i, j int) bool { return journeys[i].Name < journeys[j].Name })
	return journeys, nil
}

// journeyName returns false for a metric outside the journey naming.
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

// FixtureWeight is the fleet's measured database and metrics-store cost, in the shape
// scripts/perf-weigh-fixture.sh writes.
type FixtureWeight struct {
	DatabaseBytes int64               `json:"fixture_bytes"`
	Counts        FixtureWeightCounts `json:"counts"`
}

// FixtureWeightCounts holds the series count the fleet occupies in the metrics store.
type FixtureWeightCounts struct {
	TelemetrySeries int64 `json:"telemetry_series"`
}

// LoadFixtureWeight reads the weighing a run took beside itself.
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
