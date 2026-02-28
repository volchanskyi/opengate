// Package auth provides password hashing and JWT token management.
package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// JWTConfig holds settings for JWT token generation and validation.
type JWTConfig struct {
	Secret   string
	Issuer   string
	Duration time.Duration
}

// Claims represents the JWT claims embedded in a token.
type Claims struct {
	UserID  uuid.UUID `json:"uid"`
	Email   string    `json:"email"`
	IsAdmin bool      `json:"admin"`
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
func (c *JWTConfig) GenerateToken(userID uuid.UUID, email string, isAdmin bool) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:  userID,
		Email:   email,
		IsAdmin: isAdmin,
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
	_, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(c.Secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("validate token: %w", err)
	}
	return claims, nil
}
