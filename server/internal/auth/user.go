package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// UserID uniquely identifies a user.
type UserID = uuid.UUID

// ErrUserNotFound is returned when a Get / GetByEmail / Delete targets a
// user that does not exist.
var ErrUserNotFound = errors.New("user not found")

// User represents an authenticated user of the system.
type User struct {
	ID           UserID    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	DisplayName  string    `json:"display_name"`
	IsAdmin      bool      `json:"is_admin"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UserRepository is the outbound persistence port for users. Per ADR-021,
// the interface lives with the consuming module (auth); the Postgres
// adapter lives alongside in this package. The users table is co-owned by
// the SecurityGroup repository (it JOINs to list group members and the
// AddMember/RemoveMember path mirrors AdminGroupID membership into
// users.is_admin via syncIsAdmin) — keeping User in the same module
// preserves that locality.
type UserRepository interface {
	Upsert(ctx context.Context, u *User) error
	Get(ctx context.Context, id UserID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	List(ctx context.Context) ([]*User, error)
	Delete(ctx context.Context, id UserID) error
}
