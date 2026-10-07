package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestAuthExpiredJWTAllEndpoints(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	env.register(t, "auth-edge@example.com", "pass1234")

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
		{http.MethodGet, "/api/v1/sites"},
		{http.MethodGet, "/api/v1/devices?site_id=" + uuid.New().String()},
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
	t.Parallel()
	env := newTestEnv(t)
	ctx := defaultTenantContext()

	token := env.register(t, "tobedeleted@example.com", "pass1234")

	resp := env.doJSON(t, http.MethodGet, pathUsersMe, token, nil)
	var user struct {
		ID uuid.UUID `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&user))
	resp.Body.Close()

	require.NoError(t, testutil.NewTestUsers(t, env.store).Delete(ctx, user.ID))

	// The JWT is stateless and still validates; /me returns 404.
	resp = env.doJSON(t, http.MethodGet, pathUsersMe, token, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestAuthMalformedJWT(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	env := newTestEnv(t)

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
	t.Parallel()
	env := newTestEnv(t)

	token1 := env.register(t, "unique@example.com", "pass1234")
	assert.NotEmpty(t, token1)

	token2 := env.login(t, "unique@example.com", "pass1234")
	assert.NotEmpty(t, token2)
}
