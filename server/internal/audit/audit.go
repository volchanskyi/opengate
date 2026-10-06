// Package audit holds the audit-log repository port, its Postgres adapter and metrics decorator.
package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// UserID is the user identifier referenced by an audit event, an alias of uuid.UUID.
type UserID = uuid.UUID

// Event records a security-relevant action.
type Event struct {
	ID        int64     `json:"id"`
	UserID    UserID    `json:"user_id"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`
}

// Query specifies filters for [Repository.Query].
type Query struct {
	UserID *UserID
	Action string
	Limit  int
	Offset int
}

// Repository is the outbound persistence port for audit events.
type Repository interface {
	Write(ctx context.Context, event *Event) error
	Query(ctx context.Context, q Query) ([]*Event, error)
}
