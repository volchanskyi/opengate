package testvm

import (
	"context"
	"errors"
	"net/http"
	"testing"

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
