// Package organization owns the Organization aggregate: one customer inside a tenant.
// Every device belongs to exactly one organization.
package organization

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ID uniquely identifies an organization.
type ID = uuid.UUID

// DefaultName is the customer a tenant is given when it has none, so a device
// always has somewhere to belong and the picker always has something to offer.
const DefaultName = "Default Organization"

// MaxNameLen bounds a customer name. Names are labels in a picker; a longer one
// is a mistake or an attempt to smuggle content through a display field.
const MaxNameLen = 128

// ErrNotFound is returned when the id names no organization visible in the caller's tenant.
// A row in another tenant reads the same as a missing row.
var ErrNotFound = errors.New("organization not found")

// ErrNameTaken is returned when a tenant already has a customer by that name.
var ErrNameTaken = errors.New("organization name already used in this tenant")

// ErrNameRequired is returned when a create or rename carries an empty name.
var ErrNameRequired = errors.New("organization name is required")

// Organization is one customer inside a tenant, which comes from the caller's scope.
type Organization struct {
	ID   ID     `json:"id"`
	Name string `json:"name"`
	// ArchivedAt is set when the customer is retired; its devices and history stay.
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// Archived reports whether the customer has been retired.
func (o *Organization) Archived() bool {
	return o.ArchivedAt != nil
}

// Repository is the outbound persistence port for the Organization aggregate.
// Every operation is scoped to the caller's tenant and fails closed without one.
type Repository interface {
	// Create stores a new customer in the caller's tenant.
	Create(ctx context.Context, org *Organization) error
	// Get returns one customer by id, or ErrNotFound.
	Get(ctx context.Context, id ID) (*Organization, error)
	// List returns the caller's customers by name. includeArchived adds the
	// retired ones; without it the result is the working set.
	List(ctx context.Context, includeArchived bool) ([]*Organization, error)
	// Rename changes a customer's name.
	Rename(ctx context.Context, id ID, name string) error
	// SetArchived retires or restores a customer.
	SetArchived(ctx context.Context, id ID, archived bool) error
	// Delete removes a customer, cascading its devices and everything hanging
	// off them.
	Delete(ctx context.Context, id ID) error
	// EnsureDefault returns an organization the caller's tenant can put a device in,
	// creating the default one when the tenant has none.
	EnsureDefault(ctx context.Context) (ID, error)
}

// ValidateName returns the error a create or rename should answer with, or nil.
func ValidateName(name string) error {
	if name == "" {
		return ErrNameRequired
	}
	if len(name) > MaxNameLen {
		return ErrNameRequired
	}
	return nil
}
