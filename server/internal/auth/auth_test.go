package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testJWTConfig() JWTConfig {
	return JWTConfig{
		Secret:   "test-secret-key-at-least-32-bytes!",
		Issuer:   "opengate-test",
		Duration: 15 * time.Minute,
	}
}

func TestHashPassword(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		hash, err := HashPassword("mypassword")
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
		assert.NotEqual(t, "mypassword", hash)

		err = CheckPassword(hash, "mypassword")
		assert.NoError(t, err)
	})

	t.Run("wrong password", func(t *testing.T) {
		hash, err := HashPassword("correct")
		require.NoError(t, err)

		err = CheckPassword(hash, "wrong")
		assert.Error(t, err)
	})

	t.Run("empty password hashes", func(t *testing.T) {
		hash, err := HashPassword("")
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
	})

	t.Run("different passwords produce different hashes", func(t *testing.T) {
		h1, err := HashPassword("password1")
		require.NoError(t, err)
		h2, err := HashPassword("password2")
		require.NoError(t, err)
		assert.NotEqual(t, h1, h2)
	})
}

func TestGenerateToken(t *testing.T) {
	cfg := testJWTConfig()
	userID := uuid.New()

	t.Run("generates valid token", func(t *testing.T) {
		token, err := cfg.GenerateToken(userID, "user@example.com", false)
		require.NoError(t, err)
		assert.NotEmpty(t, token)
	})

	t.Run("token contains correct claims", func(t *testing.T) {
		token, err := cfg.GenerateToken(userID, "admin@example.com", true)
		require.NoError(t, err)

		claims, err := cfg.ValidateToken(token)
		require.NoError(t, err)
		assert.Equal(t, userID, claims.UserID)
		assert.Equal(t, "admin@example.com", claims.Email)
		assert.True(t, claims.IsAdmin)
		assert.Equal(t, "opengate-test", claims.Issuer)
	})
}

func TestValidateToken(t *testing.T) {
	cfg := testJWTConfig()
	userID := uuid.New()

	t.Run("valid token", func(t *testing.T) {
		token, err := cfg.GenerateToken(userID, "user@example.com", false)
		require.NoError(t, err)

		claims, err := cfg.ValidateToken(token)
		require.NoError(t, err)
		assert.Equal(t, userID, claims.UserID)
		assert.Equal(t, "user@example.com", claims.Email)
		assert.False(t, claims.IsAdmin)
	})

	t.Run("expired token", func(t *testing.T) {
		expiredCfg := JWTConfig{
			Secret:   cfg.Secret,
			Issuer:   cfg.Issuer,
			Duration: -1 * time.Hour, // already expired
		}
		token, err := expiredCfg.GenerateToken(userID, "user@example.com", false)
		require.NoError(t, err)

		_, err = cfg.ValidateToken(token)
		assert.Error(t, err)
	})

	t.Run("tampered token", func(t *testing.T) {
		token, err := cfg.GenerateToken(userID, "user@example.com", false)
		require.NoError(t, err)

		// flip a character in the token
		tampered := token[:len(token)-1] + "X"
		_, err = cfg.ValidateToken(tampered)
		assert.Error(t, err)
	})

	t.Run("wrong secret", func(t *testing.T) {
		token, err := cfg.GenerateToken(userID, "user@example.com", false)
		require.NoError(t, err)

		otherCfg := JWTConfig{
			Secret:   "different-secret-key-also-32-bytes!",
			Issuer:   cfg.Issuer,
			Duration: cfg.Duration,
		}
		_, err = otherCfg.ValidateToken(token)
		assert.Error(t, err)
	})

	t.Run("empty string", func(t *testing.T) {
		_, err := cfg.ValidateToken("")
		assert.Error(t, err)
	})

	t.Run("garbage string", func(t *testing.T) {
		_, err := cfg.ValidateToken("not.a.jwt")
		assert.Error(t, err)
	})
}
