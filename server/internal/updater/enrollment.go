package updater

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// EnrollmentToken authorises agent enrollment and CA certificate retrieval.
type EnrollmentToken struct {
	ID        uuid.UUID `json:"id"`
	Token     string    `json:"token"`
	Label     string    `json:"label"`
	CreatedBy uuid.UUID `json:"created_by"`
	MaxUses   int       `json:"max_uses"`
	UseCount  int       `json:"use_count"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// EnrollmentTokenRepository is the persistence port for agent enrollment tokens.
// IncrementUseCount returns an error when the token does not exist.
type EnrollmentTokenRepository interface {
	Create(ctx context.Context, t *EnrollmentToken) error
	GetByToken(ctx context.Context, token string) (*EnrollmentToken, error)
	List(ctx context.Context, createdBy uuid.UUID) ([]*EnrollmentToken, error)
	Delete(ctx context.Context, id uuid.UUID) error
	IncrementUseCount(ctx context.Context, id uuid.UUID) error
}
