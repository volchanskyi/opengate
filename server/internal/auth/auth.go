// Package auth provides password hashing and JWT token management.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"golang.org/x/crypto/bcrypt"
)

// JWTConfig holds settings for JWT token generation and validation.
type JWTConfig struct {
	Secret   string
	Issuer   string
	Duration time.Duration
}

// ErrTenantClaimMissing is returned when a token carries no readable tenant.
// Every request runs inside a tenant, so a token that names none cannot be
// honoured; it is refused as a token, which asks the caller to log in again
// instead of letting the request reach a handler with no scope to work in.
var ErrTenantClaimMissing = errors.New("token carries no tenant claim")

// Claims represents the JWT claims embedded in a token.
type Claims struct {
	UserID   uuid.UUID `json:"uid"`
	TenantID uuid.UUID `json:"tenant"`
	Email    string    `json:"email"`
	IsAdmin  bool      `json:"admin"`
	jwt.RegisteredClaims
}

// HashPassword returns a bcrypt hash of the given password.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword compares a bcrypt hash with a plaintext password.
func CheckPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// GenerateToken creates a signed JWT for the given user.
func (c *JWTConfig) GenerateToken(userID uuid.UUID, email string, isAdmin bool, tenantIDs ...uuid.UUID) (string, error) {
	now := time.Now()
	tenantID := dbtx.DefaultTenantID
	if len(tenantIDs) > 0 && tenantIDs[0] != uuid.Nil {
		tenantID = tenantIDs[0]
	}
	claims := &Claims{
		UserID:   userID,
		TenantID: tenantID,
		Email:    email,
		IsAdmin:  isAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    c.Issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(c.Duration)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(c.Secret))
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// ValidateToken parses and validates a JWT string, returning the embedded claims.
func (c *JWTConfig) ValidateToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(c.Secret), nil
	}, jwt.WithIssuer(c.Issuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, fmt.Errorf("validate token: %w", err)
	}
	if claims.TenantID == uuid.Nil {
		return nil, ErrTenantClaimMissing
	}
	return claims, nil
}
