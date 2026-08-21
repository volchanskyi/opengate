package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// The three refusals a technician meets when they ask for a session they
// cannot have. None of them needs a machine on the other end — the request is
// turned away before anything is asked of one — so they live beside the
// handler rather than in the transport tier.

const pathSessionsAPI = "/api/v1/sessions"

// TestCreateSessionForAnOfflineDeviceIsRefused pins the 409: the device is
// known, but nothing is connected to carry the session.
func TestCreateSessionForAnOfflineDeviceIsRefused(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	site := testutil.SeedSite(t, ctx, env.store)
	offline := testutil.SeedDevice(t, ctx, env.store, site.ID)

	token, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	resp := env.doJSON(t, http.MethodPost, pathSessionsAPI, token,
		map[string]any{"device_id": offline.ID.String()})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

// TestCreateSessionForAnUnknownDeviceIsNotFound pins the 404, which is also
// what a device in another tenant answers.
func TestCreateSessionForAnUnknownDeviceIsNotFound(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	user := testutil.SeedUser(t, context.Background(), env.store)
	token, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	resp := env.doJSON(t, http.MethodPost, pathSessionsAPI, token,
		map[string]any{"device_id": uuid.NewString()})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestDeleteAnUnknownSessionIsNotFound pins the last one: a token nothing was
// ever issued against.
func TestDeleteAnUnknownSessionIsNotFound(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	user := testutil.SeedUser(t, context.Background(), env.store)
	token, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	resp := env.doJSON(t, http.MethodDelete,
		pathSessionsAPI+"/nonexistent-token-that-does-not-exist-at-all-1234567890abcdef", token, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
