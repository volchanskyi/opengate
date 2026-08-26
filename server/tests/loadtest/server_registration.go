package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Registration has to be measured where the device row lands.
//
// The harness's own clock stops when the register frame has been handed to a
// local send buffer, which is a measurement of this process and not of the
// server: the row is written later, somewhere else, and the number the trend
// keeps cannot move however slow that becomes. Two ceilings sat on it.
//
// So the number comes from the server. It publishes how long registration took
// as it saw it, split by whether the registration was accepted or refused, and
// it publishes the connection pool beside it — because a registration queued
// behind a connection and one executing slowly are the same latency until the
// pool says which.

// registrationMetric is the family the server publishes registration timing in.
const registrationMetric = "opengate_agent_registration_duration_seconds"

// poolMetric is the family the server publishes pool occupancy in.
const poolMetric = "opengate_db_pool_connections"

// acceptedOutcome is the label value for a registration that completed.
const acceptedOutcome = "accepted"

// serverMetricsTimeout bounds the read. The page is small and local; a read that
// hangs would hold the end of a run for no useful reason.
const serverMetricsTimeout = 15 * time.Second

// bucketBound is one cumulative histogram bucket.
type bucketBound struct {
	le    float64
	count float64
}

// ServerRegistration is registration as the server measured it.
type ServerRegistration struct {
	// Accepted and Rejected are counts, held apart because a refused
	// registration is the system working rather than a fault.
	Accepted int64
	Rejected int64

	// SumSeconds is the total time accepted registrations took.
	SumSeconds float64

	// Buckets are the accepted outcome's cumulative buckets, in order.
	Buckets []bucketBound

	// PoolOpen, PoolInUse, PoolIdle and PoolMaxOpen are the connection pool at
	// the moment of reading.
	PoolOpen    float64
	PoolInUse   float64
	PoolIdle    float64
	PoolMaxOpen float64
}

// Measured reports whether the server saw any registration at all. A run that
// measured nothing must not read as a run that measured zero milliseconds.
func (r ServerRegistration) Measured() bool { return r.Accepted > 0 }

// MeanMs is the average accepted registration, in milliseconds.
func (r ServerRegistration) MeanMs() float64 {
	if r.Accepted == 0 {
		return 0
	}
	return r.SumSeconds / float64(r.Accepted) * 1000
}

// QuantileMs is the tail of accepted registrations, in milliseconds.
//
// The value is interpolated inside whichever bucket the quantile falls in,
// which is what a bucketed histogram can honestly say: the exact figure was
// never kept, only how many fell below each boundary. Anything past the last
// finite boundary is reported at that boundary rather than as an infinity,
// because a ceiling cannot be compared against one.
func (r ServerRegistration) QuantileMs(q float64) float64 {
	if r.Accepted == 0 || len(r.Buckets) == 0 {
		return 0
	}

	want := q * float64(r.Accepted)
	previousBound, previousCount := 0.0, 0.0
	for _, bucket := range r.Buckets {
		if bucket.count < want {
			if !math.IsInf(bucket.le, 1) {
				previousBound = bucket.le
			}
			previousCount = bucket.count
			continue
		}
		if math.IsInf(bucket.le, 1) {
			return previousBound * 1000
		}
		span := bucket.count - previousCount
		if span <= 0 {
			return bucket.le * 1000
		}
		within := (want - previousCount) / span
		return (previousBound + (bucket.le-previousBound)*within) * 1000
	}
	return previousBound * 1000
}

// FetchServerRegistration reads the running server's own account of the run.
func FetchServerRegistration(baseURL string) (ServerRegistration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), serverMetricsTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(baseURL, "/")+"/metrics", nil)
	if err != nil {
		return ServerRegistration{}, fmt.Errorf("build metrics request: %w", err)
	}

	response, err := (&http.Client{Timeout: serverMetricsTimeout}).Do(request)
	if err != nil {
		return ServerRegistration{}, fmt.Errorf("read server metrics: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return ServerRegistration{}, fmt.Errorf("read server metrics: server answered %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return ServerRegistration{}, fmt.Errorf("read server metrics: %w", err)
	}
	return ParseServerRegistration(string(body))
}

// ParseServerRegistration reads the two families this needs out of a metrics
// page. It reads only those, rather than pulling in a parser for the whole
// exposition format, so what the harness depends on is visible here.
func ParseServerRegistration(page string) (ServerRegistration, error) {
	reading := ServerRegistration{}
	buckets := map[float64]float64{}

	for _, line := range strings.Split(page, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, labels, value, ok := splitSample(line)
		if !ok {
			continue
		}

		switch {
		case name == registrationMetric+"_count":
			if labels["result"] == acceptedOutcome {
				reading.Accepted = int64(value)
			} else {
				reading.Rejected += int64(value)
			}
		case name == registrationMetric+"_sum":
			if labels["result"] == acceptedOutcome {
				reading.SumSeconds = value
			}
		case name == registrationMetric+"_bucket":
			if labels["result"] != acceptedOutcome {
				continue
			}
			bound, err := parseBound(labels["le"])
			if err != nil {
				return ServerRegistration{}, fmt.Errorf("registration histogram: %w", err)
			}
			buckets[bound] = value
		case name == poolMetric:
			reading.recordPool(labels["state"], value)
		}
	}

	reading.Buckets = orderedBuckets(buckets)
	return reading, nil
}

// recordPool files one pool gauge under the state it describes.
func (r *ServerRegistration) recordPool(state string, value float64) {
	switch state {
	case "open":
		r.PoolOpen = value
	case "in_use":
		r.PoolInUse = value
	case "idle":
		r.PoolIdle = value
	case "max_open":
		r.PoolMaxOpen = value
	}
}

// orderedBuckets puts the cumulative buckets in boundary order, which is the
// order a quantile has to walk them in and not the order a map yields.
func orderedBuckets(in map[float64]float64) []bucketBound {
	out := make([]bucketBound, 0, len(in))
	for le, count := range in {
		out = append(out, bucketBound{le: le, count: count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].le < out[j].le })
	return out
}

// splitSample breaks one exposition line into its name, its labels and its
// value. A line it cannot read is skipped rather than failing the read: the page
// carries families this does not care about and new ones arrive over time.
func splitSample(line string) (name string, labels map[string]string, value float64, ok bool) {
	labels = map[string]string{}

	open := strings.IndexByte(line, '{')
	space := strings.LastIndexByte(line, ' ')
	if space < 0 {
		return "", nil, 0, false
	}

	if open >= 0 && open < space {
		close := strings.LastIndexByte(line[:space], '}')
		if close < open {
			return "", nil, 0, false
		}
		name = line[:open]
		for _, pair := range splitLabels(line[open+1 : close]) {
			key, raw, found := strings.Cut(pair, "=")
			if !found {
				continue
			}
			labels[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(raw), `"`)
		}
	} else {
		name = strings.TrimSpace(line[:space])
	}

	parsed, err := strconv.ParseFloat(strings.TrimSpace(line[space+1:]), 64)
	if err != nil {
		return "", nil, 0, false
	}
	return name, labels, parsed, true
}

// splitLabels breaks a label set on commas that are not inside a quoted value.
func splitLabels(in string) []string {
	var pairs []string
	quoted := false
	start := 0
	for i, char := range in {
		switch char {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				pairs = append(pairs, in[start:i])
				start = i + 1
			}
		}
	}
	if start < len(in) {
		pairs = append(pairs, in[start:])
	}
	return pairs
}

// parseBound reads a bucket boundary, including the open-ended one.
func parseBound(raw string) (float64, error) {
	if raw == "+Inf" {
		return math.Inf(1), nil
	}
	bound, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("bucket boundary %q is not a number: %w", raw, err)
	}
	return bound, nil
}
