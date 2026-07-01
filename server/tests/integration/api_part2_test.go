package integration

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"net/http"
	"testing"
	"time"
)

func (e *testEnv) register(t *testing.T, email, password string) string {
	t.Helper()
	resp := e.doJSON(t, http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"email":    email,
		"password": password,
	})
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var tok tokenResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tok))
	return tok.Token
}

func (e *testEnv) login(t *testing.T, email, password string) string {
	t.Helper()
	resp := e.doJSON(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email":    email,
		"password": password,
	})
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var tok tokenResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tok))
	return tok.Token
}

func TestAuthFlow(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	t.Run("register then login then access protected endpoint", func(t *testing.T) {
		// 1. Register
		regToken := env.register(t, aliceEmail, "strongpass")
		assert.NotEmpty(t, regToken)

		// 2. Login with same credentials
		loginToken := env.login(t, aliceEmail, "strongpass")
		assert.NotEmpty(t, loginToken)

		// 3. Use token to access protected endpoint
		resp := env.doJSON(t, http.MethodGet, pathUsersMe, loginToken, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var user db.User
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&user))
		assert.Equal(t, aliceEmail, user.Email)
		assert.Empty(t, user.PasswordHash) // json:"-" omits it
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		// Generate a token that's already expired
		expiredCfg := &auth.JWTConfig{
			Secret:   env.jwt.Secret,
			Issuer:   env.jwt.Issuer,
			Duration: -1 * time.Hour,
		}
		expiredToken, err := expiredCfg.GenerateToken(uuid.New(), "expired@example.com", false)
		require.NoError(t, err)

		resp := env.doJSON(t, http.MethodGet, pathUsersMe, expiredToken, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("no token is rejected", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodGet, pathUsersMe, "", nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("wrong password fails login", func(t *testing.T) {
		resp := env.doJSON(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
			"email":    aliceEmail,
			"password": "wrongpass",
		})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}
