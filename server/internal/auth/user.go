package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// UserID uniquely identifies a user.
type UserID = uuid.UUID

// ErrUserNotFound is returned when an operation targets a missing user.
var ErrUserNotFound = errors.New("user not found")

// User represents an authenticated user of the system.
type User struct {
	ID           UserID    `json:"id"`
	TenantID     uuid.UUID `json:"tenant_id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	DisplayName  string    `json:"display_name"`
	IsAdmin      bool      `json:"is_admin"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UserRepository is the persistence port for users.
type UserRepository interface {
	Upsert(ctx context.Context, u *User) error
	Get(ctx context.Context, id UserID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	List(ctx context.Context) ([]*User, error)
	Delete(ctx context.Context, id UserID) error
}
