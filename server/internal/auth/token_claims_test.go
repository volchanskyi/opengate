package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signClaims signs an arbitrary claim set with cfg's secret, so a test can
// present a token this build would never mint.
func signClaims(t *testing.T, cfg JWTConfig, claims jwt.MapClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.Secret))
	require.NoError(t, err)
	return signed
}

// TestValidateTokenRejectsMissingTenant covers the token a caller still holds
// from before the tenancy rename: correctly signed, unexpired, and carrying no
// tenant claim this build can read. It must be refused as a token, so the caller
// is sent back to log in rather than reaching a handler with no tenant scope and
// failing somewhere far from the cause.
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

// TestValidateTokenAcceptsTenantClaim is the positive half: a token this build
// mints round-trips, so the rejection above is about the missing claim and not
// about validation being broken outright.
func TestValidateTokenAcceptsTenantClaim(t *testing.T) {
	cfg := testJWTConfig()
	tenantID := uuid.New()

	token, err := cfg.GenerateToken(uuid.New(), testEmail, false, tenantID)
	require.NoError(t, err)

	claims, err := cfg.ValidateToken(token)
	require.NoError(t, err)
	assert.Equal(t, tenantID, claims.TenantID)
}
