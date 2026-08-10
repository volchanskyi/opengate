package testvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestResolveBaseURL covers the env-override vs. auto-provision branch without
// Docker: an external VICTORIAMETRICS_TEST_URL is used verbatim (and skips
// provisioning), otherwise a container is provisioned, and a provisioning
// failure propagates instead of silently returning an empty URL.
func TestResolveBaseURL(t *testing.T) {
	provisionErr := errors.New("docker unavailable")

	tests := []struct {
		name            string
		env             string
		provisionURL    string
		provisionErr    error
		wantURL         string
		wantErr         error
		wantStartCalled bool
	}{
		{
			name:            "honors env override without provisioning",
			env:             "http://vm.example:8428",
			provisionURL:    "http://provisioned:8428",
			wantURL:         "http://vm.example:8428",
			wantStartCalled: false,
		},
		{
			name:            "provisions a container when env is unset",
			env:             "",
			provisionURL:    "http://127.0.0.1:32769",
			wantURL:         "http://127.0.0.1:32769",
			wantStartCalled: true,
		},
		{
			name:            "propagates provisioning failure",
			env:             "",
			provisionErr:    provisionErr,
			wantErr:         provisionErr,
			wantStartCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string {
				if k == URLEnv {
					return tt.env
				}
				return ""
			}
			startCalled := false
			start := func() (string, error) {
				startCalled = true
				return tt.provisionURL, tt.provisionErr
			}

			got, err := resolveBaseURL(getenv, start)

			require.Equal(t, tt.wantStartCalled, startCalled)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantURL, got)
		})
	}
}

// TestDedicated_IgnoresTheSharedURL pins the property Dedicated exists for: a
// measurement that reads VictoriaMetrics' own memory or disk must not share the
// instance with the rest of the suite, so an external URLEnv — which `make
// test-go` always sets — must not be handed back here.
func TestDedicated_IgnoresTheSharedURL(t *testing.T) {
	t.Setenv(URLEnv, "http://shared.example:8428")

	base := Dedicated(t)

	require.NotEqual(t, "http://shared.example:8428", base,
		"a dedicated VictoriaMetrics must be its own container, not the shared one")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/health", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestDedicated_HoldsOnlyItsOwnData covers what a dedicated instance is for: two
// of them, live at the same time, do not see each other's series, and neither
// sees the shared one's.
func TestDedicated_HoldsOnlyItsOwnData(t *testing.T) {
	first, second := Dedicated(t), Dedicated(t)
	require.NotEqual(t, first, second, "each call must provision its own container")

	require.NoError(t, importSample(first, `dedicated_probe{origin="first"} 1`))

	require.Equal(t, 1, waitForSeries(t, first, 1), "the store that was written to holds the series")
	require.Equal(t, 0, seriesCount(t, first, "dedicated_probe_absent"), "and nothing it was not sent")
	require.Equal(t, 0, seriesCount(t, second, "dedicated_probe"), "its neighbour holds nothing")
}

// TestDedicated_AppliesExtraArgs proves the flags a measurement pins actually
// reach the process: a measurement that fixes VictoriaMetrics' memory budget so
// the answer does not depend on the host is only a measurement if the flag
// arrives. VictoriaMetrics reports its own command line, so the check is against
// what the running process says, not against what was passed in.
func TestDedicated_AppliesExtraArgs(t *testing.T) {
	base := Dedicated(t, "-retentionPeriod=7d")

	flag := scrapeFlag(t, base, "retentionPeriod")
	require.Contains(t, flag, `value="7d"`,
		"the flag passed to Dedicated must be the one VictoriaMetrics is running with")
	require.Contains(t, flag, `is_set="true"`)
}

// scrapeFlag returns the value VictoriaMetrics reports for a command-line flag
// through its own flag gauge.
func scrapeFlag(t *testing.T, base, name string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/metrics", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	for line := range strings.SplitSeq(string(body), "\n") {
		if strings.HasPrefix(line, "flag{name=\""+name+"\"") {
			return line
		}
	}
	return ""
}

// importSample writes one exposition line into a VictoriaMetrics instance.
func importSample(base, line string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		base+"/api/v1/import/prometheus", strings.NewReader(line))
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("import returned %d", resp.StatusCode)
	}
	return nil
}

// waitForSeries flushes and re-counts until an instance holds want series, then
// returns the last reading so a mismatch fails on the real number.
func waitForSeries(t *testing.T, base string, want int) int {
	t.Helper()
	var last int
	for range 25 {
		if last = seriesCount(t, base, "dedicated_probe"); last == want {
			return last
		}
		time.Sleep(200 * time.Millisecond)
	}
	return last
}

// seriesCount returns how many series named metric an instance holds, flushing
// first so a just-written sample is visible.
func seriesCount(t *testing.T, base, metric string) int {
	t.Helper()
	flush, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/internal/force_flush", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(flush)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		base+"/api/v1/series?match[]="+url.QueryEscape(`{__name__="`+metric+`"}`), nil)
	require.NoError(t, err)
	series, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	var result struct {
		Data []map[string]string `json:"data"`
	}
	decodeErr := json.NewDecoder(series.Body).Decode(&result)
	require.NoError(t, series.Body.Close())
	require.NoError(t, decodeErr)
	return len(result.Data)
}

// TestBaseURL_StartsHealthyVictoriaMetrics provisions a throwaway VM (or uses
// the external one named by URLEnv) and asserts its /health endpoint is ready.
// It never skips: a provisioning failure fails loudly via BaseURL, so a missing
// VM is a red test rather than a false green.
func TestBaseURL_StartsHealthyVictoriaMetrics(t *testing.T) {
	base := BaseURL(t)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/health", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
}
