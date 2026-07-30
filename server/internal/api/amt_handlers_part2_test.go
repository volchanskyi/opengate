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

	group := testutil.SeedGroup(t, ctx, srv.store)
	dev := testutil.SeedDevice(t, ctx, srv.store, group.ID)
	amtDevice := testutil.SeedAMTDevice(t, ctx, srv.store, dev.ID)

	body := AMTPowerRequest{Action: HardReset}
	w := doRequest(srv, http.MethodPost, testPathAMTOne+amtDevice.UUID.String()+"/power", token, body)
	assert.Equal(t, http.StatusConflict, w.Code)

	var apiErr ApiError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	assert.Equal(t, "device not connected", apiErr.Error)
}

// TestAmtPowerActionUnknownIdentity pins the tenancy guard in front of the CIRA
// connection map: that map is keyed by AMT UUID alone and knows no tenant, so an
// identity with no managed device in the caller's scope must stop at the
// repository lookup and never reach the operator.
func TestAmtPowerActionUnknownIdentity(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "amt-unknown@example.com", true)

	body := AMTPowerRequest{Action: HardReset}
	w := doRequest(srv, http.MethodPost, testPathAMTOne+uuid.New().String()+"/power", token, body)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestAmtPowerActionRejectsOtherOrganization proves the guard is a tenant
// boundary, not just an existence check: a real AMT identity from another
// organization is refused before any command is dispatched.
func TestAmtPowerActionRejectsOtherOrganization(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	ctx := testTenantContext(t)

	group := testutil.SeedGroup(t, ctx, srv.store)
	dev := testutil.SeedDevice(t, ctx, srv.store, group.ID)
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
