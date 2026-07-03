package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/correlate"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
)

// fakeCorrelator records how the handler invoked the engine and returns a
// canned outcome.
type fakeCorrelator struct {
	gotOrg uuid.UUID
	gotReq correlate.Request
	called int
	result correlate.Result
	err    error
}

func (f *fakeCorrelator) Correlate(_ context.Context, orgID uuid.UUID, req correlate.Request) (correlate.Result, error) {
	f.gotOrg = orgID
	f.gotReq = req
	f.called++
	return f.result, f.err
}

func TestCorrelateDeviceHandler(t *testing.T) {
	srv, cfg := newTestServer(t)
	user, token := seedTestUser(t, srv, cfg, "corr@example.com", false)
	ctx := testTenantContext(t)

	group := &device.Group{ID: uuid.New(), Name: "corr-group", OwnerID: user.ID}
	require.NoError(t, srv.groups.Create(ctx, group))
	dev := &device.Device{ID: uuid.New(), GroupID: group.ID, Hostname: "corr-host", OS: "linux", Status: db.StatusOnline}
	require.NoError(t, srv.devices.Upsert(ctx, dev))

	body := map[string]any{
		"focus_start": "2026-07-02T00:10:00Z",
		"focus_end":   "2026-07-02T00:20:00Z",
	}
	path := "/api/v1/devices/" + dev.ID.String() + "/correlate"

	t.Run("503 when correlation not configured", func(t *testing.T) {
		srv.correlate = nil
		w := doRequest(srv, http.MethodPost, path, token, body)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("200 maps result and scopes to the token org, not the body", func(t *testing.T) {
		fake := &fakeCorrelator{result: correlate.Result{
			Ranked: []correlate.Ranked{{
				Metric: "mem_pct", Score: 0.9, KSStatistic: 1, AnomalyRate: 1, ShiftMagnitude: 1,
				BaselineSamples: 10, FocusSamples: 10, Labels: map[string]string{"core": "0"},
			}},
			SeriesConsidered: 3,
			SeriesTruncated:  true,
		}}
		srv.correlate = fake
		w := doRequest(srv, http.MethodPost, path, token, body)
		require.Equal(t, http.StatusOK, w.Code)

		var resp CorrelateResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		require.Len(t, resp.Ranked, 1)
		assert.Equal(t, "mem_pct", resp.Ranked[0].Metric)
		assert.Equal(t, 3, resp.SeriesConsidered)
		assert.True(t, resp.SeriesTruncated)
		require.NotNil(t, resp.Ranked[0].Labels)
		assert.Equal(t, "0", (*resp.Ranked[0].Labels)["core"])

		// The engine is scoped by the authenticated tenant org and the path device.
		assert.Equal(t, 1, fake.called)
		assert.Equal(t, dbtx.DefaultOrgID, fake.gotOrg)
		assert.Equal(t, dev.ID, fake.gotReq.DeviceID)
	})

	t.Run("404 for unknown device does not reach the engine", func(t *testing.T) {
		fake := &fakeCorrelator{}
		srv.correlate = fake
		w := doRequest(srv, http.MethodPost, "/api/v1/devices/"+uuid.New().String()+"/correlate", token, body)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Equal(t, 0, fake.called)
	})

	t.Run("400 on invalid window", func(t *testing.T) {
		srv.correlate = &fakeCorrelator{err: correlate.ErrInvalidWindow}
		w := doRequest(srv, http.MethodPost, path, token, body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("503 when the engine is at capacity", func(t *testing.T) {
		srv.correlate = &fakeCorrelator{err: correlate.ErrBusy}
		w := doRequest(srv, http.MethodPost, path, token, body)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}
