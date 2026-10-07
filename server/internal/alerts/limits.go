package alerts

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const (
	// DefaultOrganizationHourlyCeiling is how many alerts one customer may store per rolling hour.
	// The budget is per customer so one customer's storm cannot consume another's under the same MSP.
	DefaultOrganizationHourlyCeiling = 500
	// MaxOrganizationHourlyCeiling is as far as that budget may be raised.
	MaxOrganizationHourlyCeiling = 5_000

	// DefaultDeviceHourlyCeiling is how many alerts one machine may raise per rolling hour.
	// The machine enforces it, so a server-side check would receive the flood it prevents.
	DefaultDeviceHourlyCeiling = 20
	// MaxDeviceHourlyCeiling is as far as that budget may be raised.
	MaxDeviceHourlyCeiling = 200
)

// ErrInvalidLimits means a budget is outside what may be stored.
var ErrInvalidLimits = errors.New("alert budget is outside its bounds")

// Limits is one customer's alert budget.
type Limits struct {
	OrganizationID uuid.UUID
	// OrganizationHourly is the customer's rolling-hour budget across every machine.
	OrganizationHourly int
	// DeviceHourly is one machine's rolling-hour budget.
	DeviceHourly int
	// UpdatedBy names whoever last moved the budget.
	UpdatedBy string
}

// DefaultLimits is the shipped budget for a customer with no stored row, which is never zero.
func DefaultLimits(organizationID uuid.UUID) Limits {
	return Limits{
		OrganizationID:     organizationID,
		OrganizationHourly: DefaultOrganizationHourlyCeiling,
		DeviceHourly:       DefaultDeviceHourlyCeiling,
	}
}

// ValidateLimits bounds a budget before it is stored; a budget of nothing would silence detection.
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
