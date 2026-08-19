package alerts

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// A customer's alert budget.
//
// Both numbers were chosen from an estimate of a rate nobody had measured — the
// customer's is roughly twelve times a steady rate that has never been observed
// on a live estate, and the machine's is the same guess made smaller. Being
// unable to move either without cutting a release turns a wrong guess into an
// outage, so both are the customer's to set.
//
// What is not the customer's is how far. Each has a maximum that lives in the
// code rather than beside the value, because a limit an operator can raise is
// not a limit: past these, one customer's storm stops being their own problem
// and starts costing the database every other customer's detection.

const (
	// DefaultOrganizationHourlyCeiling is how many alerts one customer may store
	// per rolling hour before the excess is refused and counted.
	//
	// Per customer, never per tenant: at the tenant, one customer's bad night
	// would consume the budget of every other customer the MSP looks after, and
	// silencing detection across an estate is a worse failure than the storm.
	DefaultOrganizationHourlyCeiling = 500
	// MaxOrganizationHourlyCeiling is as far as that budget may be raised.
	MaxOrganizationHourlyCeiling = 5_000

	// DefaultDeviceHourlyCeiling is how many alerts one machine may raise per
	// rolling hour. It is enforced on the machine, so it travels down with the
	// rules rather than being applied when they arrive — a server-side check
	// would receive the flood it exists to prevent.
	DefaultDeviceHourlyCeiling = 20
	// MaxDeviceHourlyCeiling is as far as that budget may be raised.
	MaxDeviceHourlyCeiling = 200
)

// ErrInvalidLimits means a budget is outside what may be stored.
var ErrInvalidLimits = errors.New("alert budget is outside its bounds")

// Limits is one customer's alert budget.
type Limits struct {
	OrganizationID uuid.UUID
	// OrganizationHourly is the customer's rolling-hour budget across every
	// machine they have.
	OrganizationHourly int
	// DeviceHourly is one of their machines' rolling-hour budget.
	DeviceHourly int
	// UpdatedBy names whoever last moved it, so a budget nobody remembers
	// raising can still be traced.
	UpdatedBy string
}

// DefaultLimits is what applies to a customer who has set nothing. A missing row
// is "not configured", never a budget of zero.
func DefaultLimits(organizationID uuid.UUID) Limits {
	return Limits{
		OrganizationID:     organizationID,
		OrganizationHourly: DefaultOrganizationHourlyCeiling,
		DeviceHourly:       DefaultDeviceHourlyCeiling,
	}
}

// ValidateLimits bounds a budget before it is stored. A budget of nothing is
// refused as firmly as one past the maximum: it would silence a customer's
// detection entirely, which is not a setting anybody means to reach.
func ValidateLimits(l Limits) error {
	if l.OrganizationID == uuid.Nil {
		return fmt.Errorf("%w: a budget belongs to a customer", ErrInvalidLimits)
	}
	for _, budget := range []struct {
		what  string
		value int
		max   int
	}{
		{"the customer's hourly budget", l.OrganizationHourly, MaxOrganizationHourlyCeiling},
		{"a machine's hourly budget", l.DeviceHourly, MaxDeviceHourlyCeiling},
	} {
		if budget.value < 1 || budget.value > budget.max {
			return fmt.Errorf("%w: %s is %d, outside 1–%d",
				ErrInvalidLimits, budget.what, budget.value, budget.max)
		}
	}
	return nil
}
