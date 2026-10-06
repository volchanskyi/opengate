package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewVMClientDefaultTimeout(t *testing.T) {
	t.Parallel()
	c := NewVMClient("http://vm.invalid", nil)
	require.NotNil(t, c.client)
	assert.Equal(t, 30*time.Second, c.client.Timeout)
}

func TestWriteSamplesStatusBoundaries(t *testing.T) {
	t.Parallel()
	samples := []Sample{{Name: "opengate_edge_metric_avg", Value: 1, TS: time.Unix(1_700_000_000, 0)}}
	cases := []struct {
		code    int
		wantErr bool
	}{
		{http.StatusOK, false},
		{299, false},
		{http.StatusMultipleChoices, true},
		{http.StatusInternalServerError, true},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.code)
		}))
		client := NewVMClient(srv.URL, nil)
		err := client.WriteSamples(context.Background(), uuid.New(), uuid.New(), samples)
		if tc.wantErr {
			require.Errorf(t, err, "status %d must be an error", tc.code)
		} else {
			require.NoErrorf(t, err, "status %d must succeed", tc.code)
		}
		srv.Close()
	}
}

func TestQueryRangeZeroStepMessage(t *testing.T) {
	t.Parallel()
	client := NewVMClient("http://127.0.0.1:0", nil)
	start := time.Unix(1_700_000_000, 0)
	_, err := client.QueryRange(context.Background(), uuid.New(), RangeQuery{
		Metric: "opengate_edge_metric_avg", Agg: RangeAvg,
		Start: start, End: start.Add(time.Hour), Step: 0,
	})
	require.ErrorContains(t, err, "step must be positive")
}

func TestBuildSelectorNoMatchers(t *testing.T) {
	t.Parallel()
	got, err := buildSelector("opengate_edge_metric_avg", nil)
	require.NoError(t, err)
	assert.Equal(t, "opengate_edge_metric_avg", got)

	got, err = buildSelector("opengate_edge_metric_avg", map[string]string{"dim": "cpu"})
	require.NoError(t, err)
	assert.Equal(t, `opengate_edge_metric_avg{dim="cpu"}`, got)
}
