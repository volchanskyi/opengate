// Package settings resolves a configurable value along the tenancy ladder:
// machine, site, customer, tenant, then the shipped default.
package settings

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Level names one rung of the tenancy ladder, ordered narrowest first.
type Level int

// The rungs, narrowest first; LevelShipped applies when no stored rung carries a value.
const (
	LevelDevice Level = iota
	LevelSite
	LevelOrganization
	LevelTenant
	LevelShipped
)

// String names the level for an operator, so a resolved value can always be
// shown alongside where it came from.
func (l Level) String() string {
	switch l {
	case LevelDevice:
		return "device"
	case LevelSite:
		return "site"
	case LevelOrganization:
		return "organization"
	case LevelTenant:
		return "tenant"
	case LevelShipped:
		return "shipped"
	default:
		return "unknown"
	}
}

// storableLevels are the rungs a value can be set on, narrowest first.
var storableLevels = []Level{LevelDevice, LevelSite, LevelOrganization, LevelTenant}

// ErrDeviceNotFound is returned when no device in the caller's tenant has the
// given id, so no ladder can be built for it.
var ErrDeviceNotFound = errors.New("device not found")

// Scope is one machine's place in the tenancy ladder. SiteID is the zero value
// when the machine is filed into no site, which removes that rung.
type Scope struct {
	DeviceID       uuid.UUID
	SiteID         uuid.UUID
	OrganizationID uuid.UUID
	TenantID       uuid.UUID
}

// Key returns the id a value would be stored against at the given level, and
// whether this scope has that rung at all.
func (s Scope) Key(level Level) (uuid.UUID, bool) {
	var id uuid.UUID
	switch level {
	case LevelDevice:
		id = s.DeviceID
	case LevelSite:
		id = s.SiteID
	case LevelOrganization:
		id = s.OrganizationID
	case LevelTenant:
		id = s.TenantID
	case LevelShipped:
		return uuid.Nil, false
	default:
		return uuid.Nil, false
	}
	return id, id != uuid.Nil
}

// Override is one stored value, the rung it was set on, and the id of the thing
// on that rung it was set for.
type Override[T any] struct {
	Level   Level
	ScopeID uuid.UUID
	Value   T
}

// Direction says which end of the ladder wins when a value is set on more than
// one rung.
type Direction int

const (
	// NarrowestWins is the ordinary rule: the machine beats its site, the site
	// beats its customer, the customer beats the tenant.
	NarrowestWins Direction = iota
	// BroadestWins serves values that stop something, so a customer-wide stop
	// outranks a value set on one machine.
	BroadestWins
)

// Reader resolves a machine's place in the tenancy ladder.
type Reader interface {
	// ScopeFor returns the ladder for one device. Returns ErrDeviceNotFound when
	// no device in the caller's tenant has that id.
	ScopeFor(ctx context.Context, deviceID uuid.UUID) (Scope, error)
}

// Resolve returns the value for scope and the rung that supplied it, counting only
// overrides on this scope's own ladder; with none the shipped default applies.
func Resolve[T any](scope Scope, overrides []Override[T], shipped T, direction Direction) (T, Level) {
	for _, level := range ladder(direction) {
		key, present := scope.Key(level)
		if !present {
			continue
		}
		for _, o := range overrides {
			if o.Level == level && o.ScopeID == key {
				return o.Value, level
			}
		}
	}
	return shipped, LevelShipped
}

// ladder returns the rungs in the order the direction reads them.
func ladder(direction Direction) []Level {
	if direction == BroadestWins {
		reversed := make([]Level, len(storableLevels))
		for i, level := range storableLevels {
			reversed[len(storableLevels)-1-i] = level
		}
		return reversed
	}
	return storableLevels
}
