// Package db provides the database abstraction layer backed by PostgreSQL.
package db

import (
	"context"
	"errors"
)

// ErrNotFound indicates the requested record does not exist.
var ErrNotFound = errors.New("not found")

// Store defines the database operations for all persistent data.
type Store interface {
	// Health
	Ping(ctx context.Context) error
	Close() error
}
