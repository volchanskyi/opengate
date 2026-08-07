// Package settings owns the tenancy ladder a configurable value is resolved
// along: a machine, the site it is filed into, the customer that site belongs
// to, the tenant above them, and finally what shipped.
//
// It holds the walk and the tie-break, and nothing else. Where a value is
// stored belongs to whatever feature the value configures — the rule catalogue
// keeps its own thresholds — so the ordering exists once and cannot drift
// between the things that depend on it.
package settings

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Level names one rung of the tenancy ladder, ordered narrowest first.
type Level int

// The rungs, narrowest first. LevelShipped is the floor: it is not a rung
// anything can be stored against, it is what applies when no rung carries a
// value.
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
// when nobody has filed the machine into a site, which removes that rung rather
// than failing.
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
	// BroadestWins is for values whose whole purpose is to stop something. A
	// customer-wide stop must not be undone by a value someone set on one
	// machine, so that class reads the ladder the other way up. Naming the
	// exception is the point: it is a decision, not an accident of ordering.
	BroadestWins
)

// Reader resolves a machine's place in the tenancy ladder.
type Reader interface {
	// ScopeFor returns the ladder for one device. Returns ErrDeviceNotFound when
	// no device in the caller's tenant has that id.
	ScopeFor(ctx context.Context, deviceID uuid.UUID) (Scope, error)
}

// Resolve returns the value that applies to scope and the rung that supplied
// it. Only overrides set on this scope's own ladder count, so a caller may hand
// over a whole customer's overrides without another machine's number reaching
// this one. When no rung carries a value the shipped default applies.
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
