package main

import (
	"bytes"
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheBenchmarkCountsFixedWorkOverItsWindow(t *testing.T) {
	result := run(200 * time.Millisecond)

	assert.Positive(t, result.Rounds)
	assert.GreaterOrEqual(t, result.Seconds, 0.2)
	assert.InDelta(t, float64(result.Rounds)/result.Seconds, result.RoundsPerSecond, 0.001)
	assert.Equal(t, runtime.GOARCH, result.Arch)
	assert.Equal(t, runtime.Version(), result.Go)
}

func TestARoundIsTheSameWorkEveryTime(t *testing.T) {
	assert.Equal(t, round(), round(), "each round hashes and compresses the same bytes")
}

func TestTheResultIsWrittenAsJSON(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, write(&out, Result{Rounds: 10, Seconds: 1, RoundsPerSecond: 10, Arch: "arm64", Go: "go1.27.3"}))
	var back map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &back))
	assert.InDelta(t, 10, back["rounds_per_second"], 0)
	assert.Equal(t, "arm64", back["arch"])
}
