package auth

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestValidateToken(t *testing.T) {
	cfg := testJWTConfig()
	userID := uuid.New()

	t.Run("valid token", func(t *testing.T) {
		token, err := cfg.GenerateToken(userID, testEmail, false)
		require.NoError(t, err)

		claims, err := cfg.ValidateToken(token)
		require.NoError(t, err)
		assert.Equal(t, userID, claims.UserID)
		assert.Equal(t, testEmail, claims.Email)
		assert.False(t, claims.IsAdmin)
	})

	t.Run("expired token", func(t *testing.T) {
		expiredCfg := JWTConfig{
			Secret:   cfg.Secret,
			Issuer:   cfg.Issuer,
			Duration: -1 * time.Hour, // already expired
		}
		token, err := expiredCfg.GenerateToken(userID, testEmail, false)
		require.NoError(t, err)

		_, err = cfg.ValidateToken(token)
		assert.Error(t, err)
	})

	t.Run("tampered token", func(t *testing.T) {
		token, err := cfg.GenerateToken(userID, testEmail, false)
		require.NoError(t, err)

		// flip a character in the middle of the signature to ensure corruption
		mid := len(token) / 2
		flipped := token[mid] ^ 0x01
		tampered := token[:mid] + string(rune(flipped)) + token[mid+1:]
		_, err = cfg.ValidateToken(tampered)
		assert.Error(t, err)
	})

	t.Run("wrong secret", func(t *testing.T) {
		token, err := cfg.GenerateToken(userID, testEmail, false)
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
