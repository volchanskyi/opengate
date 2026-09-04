package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func controlFor(t *testing.T) (*Shaper, http.Handler) {
	t.Helper()
	shaper, _, _ := startShaper(t, 5)
	return shaper, NewControl(shaper)
}

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

// The runner commands a scenario through this endpoint, so what it sends has to
// arrive as the impairment the scenario named.
func TestControlAppliesTheProfileItIsGiven(t *testing.T) {
	t.Parallel()
	shaper, h := controlFor(t)

	rec := post(t, h, "/impair", `{"blackhole":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, shaper.Counters().Profile.Blackhole)

	rec = post(t, h, "/impair", `{"rate_bits_per_sec":2000000,"max_queue_ms":1000}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got := shaper.Counters().Profile
	assert.False(t, got.Blackhole, "a new instruction left the previous impairment in force")
	assert.Equal(t, int64(2_000_000), got.RateBitsPerSec)
	assert.Equal(t, time.Second, got.MaxQueue)
}

// An instruction the shaper cannot honour must be refused rather than silently
// reduced to something it can. A scenario running a different impairment from
// the one it named produces a measurement of nothing in particular.
func TestControlRefusesAnImpairmentItCannotRun(t *testing.T) {
	t.Parallel()
	_, h := controlFor(t)

	for name, body := range map[string]string{
		"loss above one":       `{"loss_to_server":1.5}`,
		"a backwards delay":    `{"delay_each_way_ms":-1}`,
		"a rate with no queue": `{"rate_bits_per_sec":2000000}`,
		"not an instruction":   `{`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := post(t, h, "/impair", body)
			assert.Equal(t, http.StatusBadRequest, rec.Code, "an impossible instruction was accepted")
		})
	}
}

func TestControlRebinds(t *testing.T) {
	t.Parallel()
	shaper, h := controlFor(t)
	require.Equal(t, http.StatusOK, post(t, h, "/rebind", "").Code)
	assert.Equal(t, int64(1), shaper.Counters().Rebinds)
}

// The runner reads the counters at every phase boundary, so they are served as
// the machine-readable record the evidence bundle keeps.
func TestControlReportsTheCounters(t *testing.T) {
	t.Parallel()
	_, h := controlFor(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/counters", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got Counters
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, uint64(5), got.Seed, "the counters did not carry the run's seed")
}

// A runner that reaches the wrong verb is a runner that thinks it commanded a
// scenario it did not command, and the phase it measures is the phase before.
func TestControlRefusesTheWrongVerb(t *testing.T) {
	t.Parallel()
	_, h := controlFor(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/impair", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// Whether the shaper is answering at all is the difference between a scenario
// that measured the product and one that measured nothing, so it is asked
// directly rather than inferred from a counter that happens to come back.
func TestControlAnswersAHealthCheck(t *testing.T) {
	t.Parallel()
	_, h := controlFor(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}
