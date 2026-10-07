package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mintHS384(t *testing.T, cfg JWTConfig, userID uuid.UUID) string {
	t.Helper()
	claims := &Claims{
		UserID: userID,
		Email:  testEmail,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    cfg.Issuer,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(cfg.Duration)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS384, claims).SignedString([]byte(cfg.Secret))
	require.NoError(t, err)
	return signed
}

func TestValidateTokenHardening(t *testing.T) {
	t.Parallel()
	cfg := testJWTConfig()
	userID := uuid.New()

	t.Run("rejects wrong issuer", func(t *testing.T) {
		t.Parallel()
		evil := JWTConfig{Secret: cfg.Secret, Issuer: "evil-issuer", Duration: cfg.Duration}
		token, err := evil.GenerateToken(userID, testEmail, false)
		require.NoError(t, err)

		_, err = cfg.ValidateToken(token)
		assert.Error(t, err)
	})

	t.Run("rejects non-HS256 signing method", func(t *testing.T) {
		t.Parallel()
		_, err := cfg.ValidateToken(mintHS384(t, cfg, userID))
		assert.Error(t, err)
	})

	t.Run("accepts a correctly-issued HS256 token", func(t *testing.T) {
		t.Parallel()
		token, err := cfg.GenerateToken(userID, testEmail, false)
		require.NoError(t, err)

		claims, err := cfg.ValidateToken(token)
		require.NoError(t, err)
		assert.Equal(t, cfg.Issuer, claims.Issuer)
	})
}
