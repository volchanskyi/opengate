package testvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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

func TestDedicated_IgnoresTheSharedURL(t *testing.T) {
	t.Setenv(URLEnv, "http://shared.example:8428")

	base := Dedicated(t)

	require.NotEqual(t, "http://shared.example:8428", base,
		"a dedicated VictoriaMetrics must be its own container, not the shared one")

	status, _ := httpGet(t, base+"/health")
	require.Equal(t, http.StatusOK, status)
}

func TestDedicated_HoldsOnlyItsOwnData(t *testing.T) {
	first, second := Dedicated(t), Dedicated(t)
	require.NotEqual(t, first, second, "each call must provision its own container")

	require.NoError(t, importSample(first, `dedicated_probe{origin="first"} 1`))

	require.Equal(t, 1, waitForSeries(t, first, 1), "the store that was written to holds the series")
	require.Equal(t, 0, seriesCount(t, first, "dedicated_probe_absent"), "and nothing it was not sent")
	require.Equal(t, 0, seriesCount(t, second, "dedicated_probe"), "its neighbour holds nothing")
}

func TestDedicated_AppliesExtraArgs(t *testing.T) {
	base := Dedicated(t, "-retentionPeriod=7d")

	flag := scrapeFlag(t, base, "retentionPeriod")
	require.Contains(t, flag, `value="7d"`,
		"the flag passed to Dedicated must be the one VictoriaMetrics is running with")
	require.Contains(t, flag, `is_set="true"`)
}

func httpGet(t *testing.T, target string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, readErr := io.ReadAll(resp.Body)
	require.NoError(t, resp.Body.Close())
	require.NoError(t, readErr)
	return resp.StatusCode, body
}

func scrapeFlag(t *testing.T, base, name string) string {
	t.Helper()
	_, body := httpGet(t, base+"/metrics")

	for line := range strings.SplitSeq(string(body), "\n") {
		if strings.HasPrefix(line, "flag{name=\""+name+"\"") {
			return line
		}
	}
	return ""
}

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

func seriesCount(t *testing.T, base, metric string) int {
	t.Helper()
	httpGet(t, base+"/internal/force_flush")

	_, body := httpGet(t, base+"/api/v1/series?match[]="+url.QueryEscape(`{__name__="`+metric+`"}`))
	var result struct {
		Data []map[string]string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &result))
	return len(result.Data)
}

func TestBaseURL_StartsHealthyVictoriaMetrics(t *testing.T) {
	status, _ := httpGet(t, BaseURL(t)+"/health")

	require.Equal(t, http.StatusOK, status)
}

func TestPackageSettlesTheReaper(t *testing.T) {
	require.NotEmpty(t, os.Getenv("TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT"),
		"importing testvm must widen the reaper wait")
	require.NotEmpty(t, os.Getenv("TESTCONTAINERS_SESSION_ID"),
		"importing testvm must give this process a reaper of its own")
}
