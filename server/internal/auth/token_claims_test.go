package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func signClaims(t *testing.T, cfg JWTConfig, claims jwt.MapClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.Secret))
	require.NoError(t, err)
	return signed
}

func TestValidateTokenRejectsMissingTenant(t *testing.T) {
	cfg := testJWTConfig()
	now := time.Now()

	tests := []struct {
		name   string
		tenant any
	}{
		{"claim absent", nil},
		{"claim is the nil uuid", uuid.Nil.String()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			claims := jwt.MapClaims{
				"uid":   uuid.New().String(),
				"email": testEmail,
				"admin": false,
				"iss":   cfg.Issuer,
				"iat":   now.Unix(),
				"exp":   now.Add(cfg.Duration).Unix(),
			}
			if tc.tenant != nil {
				claims["tenant"] = tc.tenant
			}

			got, err := cfg.ValidateToken(signClaims(t, cfg, claims))
			require.ErrorIs(t, err, ErrTenantClaimMissing)
			assert.Nil(t, got)
		})
	}
}

func TestValidateTokenAcceptsTenantClaim(t *testing.T) {
	cfg := testJWTConfig()
	tenantID := uuid.New()

	token, err := cfg.GenerateToken(uuid.New(), testEmail, false, tenantID)
	require.NoError(t, err)

	claims, err := cfg.ValidateToken(token)
	require.NoError(t, err)
	assert.Equal(t, tenantID, claims.TenantID)
}
