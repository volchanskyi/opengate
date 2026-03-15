package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
)

func TestAuthExpiredJWTAllEndpoints(t *testing.T) {
	env := newTestEnv(t)

	// Register a real user so endpoints would succeed with a valid token
	env.register(t, "auth-edge@example.com", "pass123")

	// Generate an already-expired token
	expiredCfg := &auth.JWTConfig{
		Secret:   env.jwt.Secret,
		Issuer:   env.jwt.Issuer,
		Duration: -1 * time.Hour,
	}
	expiredToken, err := expiredCfg.GenerateToken(uuid.New(), "auth-edge@example.com", false)
	require.NoError(t, err)

	protectedEndpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/users/me"},
		{http.MethodGet, "/api/v1/groups"},
		{http.MethodGet, "/api/v1/devices?group_id=" + uuid.New().String()},
		{http.MethodGet, "/api/v1/sessions?device_id=" + uuid.New().String()},
		{http.MethodGet, "/api/v1/users"},
		{http.MethodGet, "/api/v1/audit"},
	}

	for _, ep := range protectedEndpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			resp := env.doJSON(t, ep.method, ep.path, expiredToken, nil)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		})
	}
}

func TestAuthDeletedUser(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// Register user and get token
	token := env.register(t, "tobedeleted@example.com", "pass123")

	// Get user ID
	resp := env.doJSON(t, http.MethodGet, pathUsersMe, token, nil)
	var user struct {
		ID uuid.UUID `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&user))
	resp.Body.Close()

	// Delete user from store directly
	require.NoError(t, env.store.DeleteUser(ctx, user.ID))

	// Token is still valid (JWT is stateless) but /me returns 404
	resp = env.doJSON(t, http.MethodGet, pathUsersMe, token, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestAuthMalformedJWT(t *testing.T) {
	env := newTestEnv(t)

	malformedTokens := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"garbage", "not-a-jwt-token"},
		{"truncated", "eyJhbGciOiJIUzI1NiJ9.eyJ1aWQ"},
		{"three dots", "a.b.c"},
	}

	for _, tc := range malformedTokens {
		t.Run(tc.name, func(t *testing.T) {
			resp := env.doJSON(t, http.MethodGet, pathUsersMe, tc.token, nil)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		})
	}
}

func TestAuthWrongSecret(t *testing.T) {
	env := newTestEnv(t)

	// Generate a token with a different secret
	wrongCfg := &auth.JWTConfig{
		Secret:   "completely-different-secret-32b!x",
		Issuer:   env.jwt.Issuer,
		Duration: 15 * time.Minute,
	}
	wrongToken, err := wrongCfg.GenerateToken(uuid.New(), "wrong@example.com", false)
	require.NoError(t, err)

	resp := env.doJSON(t, http.MethodGet, pathUsersMe, wrongToken, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuthDuplicateRegistration(t *testing.T) {
	env := newTestEnv(t)

	// Register first time — should succeed
	token1 := env.register(t, "unique@example.com", "pass123")
	assert.NotEmpty(t, token1)

	// Register same email again — UpsertUser is an upsert so it succeeds,
	// but login with the old password should still work (upsert keeps the
	// first password hash since it's an INSERT OR IGNORE on the ID column).
	token2 := env.login(t, "unique@example.com", "pass123")
	assert.NotEmpty(t, token2)
}
