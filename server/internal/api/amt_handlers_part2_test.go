package api

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"net/http"
	"testing"
)

func TestAmtPowerActionNotConnected(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, testAMTEmail, true)
	ctx := testTenantContext(t)

	site := testutil.SeedSite(t, ctx, srv.store)
	dev := testutil.SeedDevice(t, ctx, srv.store, site.ID)
	amtDevice := testutil.SeedAMTDevice(t, ctx, srv.store, dev.ID)

	body := AMTPowerRequest{Action: HardReset}
	w := doRequest(srv, http.MethodPost, testPathAMTOne+amtDevice.UUID.String()+"/power", token, body)
	assert.Equal(t, http.StatusConflict, w.Code)

	var apiErr ApiError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	assert.Equal(t, "device not connected", apiErr.Error)
}

func TestAmtPowerActionUnknownIdentity(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "amt-unknown@example.com", true)

	body := AMTPowerRequest{Action: HardReset}
	w := doRequest(srv, http.MethodPost, testPathAMTOne+uuid.New().String()+"/power", token, body)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAmtPowerActionRejectsOtherTenant(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	ctx := testTenantContext(t)

	site := testutil.SeedSite(t, ctx, srv.store)
	dev := testutil.SeedDevice(t, ctx, srv.store, site.ID)
	amtDevice := testutil.SeedAMTDevice(t, ctx, srv.store, dev.ID)

	outsider, _ := seedTestUser(t, srv, cfg, "amt-outsider@example.com", false)
	outsiderToken, err := cfg.GenerateToken(outsider.ID, outsider.Email, false, uuid.New())
	require.NoError(t, err)

	body := AMTPowerRequest{Action: HardReset}
	w := doRequest(srv, http.MethodPost, testPathAMTOne+amtDevice.UUID.String()+"/power", outsiderToken, body)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAmtPowerActionUnauthorized(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	body := AMTPowerRequest{Action: PowerOn}
	w := doRequest(srv, http.MethodPost, testPathAMTOne+uuid.New().String()+"/power", "", body)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
