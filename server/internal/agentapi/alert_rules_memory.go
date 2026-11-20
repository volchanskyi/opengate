package agentapi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/rules"
)

// CustomerRef names one customer and the tenant whose boundary its settings are read in.
type CustomerRef struct {
	TenantID       uuid.UUID
	OrganizationID uuid.UUID
}

// CustomerDirectory lists the customers whose rule settings the server holds in memory.
type CustomerDirectory interface {
	// AllCustomers lists every tenant's customers, under an administrator scope.
	AllCustomers(ctx context.Context) ([]CustomerRef, error)
	// CustomersIn lists the customers of the caller's tenant.
	CustomersIn(ctx context.Context) ([]uuid.UUID, error)
}

// heldSettings is what one customer changed about its rules as last read, and that read's stamp.
type heldSettings struct {
	bindings     []rules.Binding
	rollouts     map[string]rules.Rollout
	deviceHourly uint32
	version      uint64
}

// settingsMemory holds the customer settings read so far; a later read of a customer replaces it.
type settingsMemory struct {
	mu         sync.RWMutex
	byCustomer map[uuid.UUID]heldSettings
	stamps     atomic.Uint64
}

func (m *settingsMemory) held(organizationID uuid.UUID) (heldSettings, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.byCustomer[organizationID]
	return s, ok
}

// hold keeps s for the customer; when first is set, a read already held since wins over s.
func (m *settingsMemory) hold(organizationID uuid.UUID, s heldSettings, first bool) heldSettings {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byCustomer == nil {
		m.byCustomer = make(map[uuid.UUID]heldSettings)
	}
	if prior, ok := m.byCustomer[organizationID]; ok && first {
		return prior
	}
	m.byCustomer[organizationID] = s
	return s
}

// Load reads every customer's settings, each inside its own tenant, before the server takes
// machines; a customer that cannot be read fails the start.
func (p *CatalogueAlertRuleProvider) Load(ctx context.Context, directory CustomerDirectory) error {
	customers, err := directory.AllCustomers(dbtx.WithDefaultTenant(ctx, true))
	if err != nil {
		return fmt.Errorf("list customers: %w", err)
	}
	for _, c := range customers {
		if err := p.Reload(dbtx.WithTenant(ctx, c.TenantID, false), c.OrganizationID); err != nil {
			return fmt.Errorf("load customer %s: %w", c.OrganizationID, err)
		}
	}
	return nil
}

// Reload reads one customer's settings again inside the caller's tenant; a read that fails keeps
// what was held.
func (p *CatalogueAlertRuleProvider) Reload(ctx context.Context, organizationID uuid.UUID) error {
	s, complete, err := p.readSettings(ctx, organizationID)
	if err != nil {
		return err
	}
	if !complete {
		return fmt.Errorf("read customer alert budget for %s", organizationID)
	}
	p.memory.hold(organizationID, s, false)
	return nil
}

// ReloadTenant reads again the settings of every customer of the caller's tenant.
func (p *CatalogueAlertRuleProvider) ReloadTenant(ctx context.Context, directory CustomerDirectory) error {
	customers, err := directory.CustomersIn(ctx)
	if err != nil {
		return fmt.Errorf("list the tenant's customers: %w", err)
	}
	var failed []error
	for _, organizationID := range customers {
		if err := p.Reload(ctx, organizationID); err != nil {
			failed = append(failed, err)
		}
	}
	return errors.Join(failed...)
}

// NeedsFleetSize reports, from memory alone, whether a customer's rollouts size a stage by its
// machine count; a customer the memory lacks has changed nothing since the start.
func (p *CatalogueAlertRuleProvider) NeedsFleetSize(organizationID uuid.UUID) bool {
	s, ok := p.memory.held(organizationID)
	return ok && rules.NeedsFleetSize(s.rollouts)
}

// Version is the stamp of the customer's settings as held, zero for a customer not yet read.
func (p *CatalogueAlertRuleProvider) Version(organizationID uuid.UUID) uint64 {
	s, _ := p.memory.held(organizationID)
	return s.version
}

// settingsFor is the customer's held settings, read inside the caller's tenant on first use.
func (p *CatalogueAlertRuleProvider) settingsFor(ctx context.Context, organizationID uuid.UUID) (heldSettings, error) {
	if s, ok := p.memory.held(organizationID); ok {
		return s, nil
	}
	s, complete, err := p.readSettings(ctx, organizationID)
	if err != nil || !complete {
		return s, err
	}
	return p.memory.hold(organizationID, s, true), nil
}

// readSettings reads one customer's bindings, rollouts and per-machine alert allowance; complete
// is false when the allowance could not be read, which leaves it zero.
func (p *CatalogueAlertRuleProvider) readSettings(ctx context.Context, organizationID uuid.UUID) (heldSettings, bool, error) {
	bindings, err := p.store.ListBindings(ctx, organizationID)
	if err != nil {
		return heldSettings{}, false, fmt.Errorf("read rule bindings: %w", err)
	}
	rollouts, err := p.store.ListRollouts(ctx, organizationID)
	if err != nil {
		return heldSettings{}, false, fmt.Errorf("read rule rollout: %w", err)
	}
	ceiling, complete := p.ceilingFor(ctx, organizationID)
	return heldSettings{
		bindings:     bindings,
		rollouts:     rollouts,
		deviceHourly: ceiling,
		version:      p.memory.stamps.Add(1),
	}, complete, nil
}

// ceilingFor reads the customer's per-machine alert allowance; zero keeps the current one, and
// complete is false when the budget could not be read.
func (p *CatalogueAlertRuleProvider) ceilingFor(ctx context.Context, organizationID uuid.UUID) (uint32, bool) {
	if p.limits == nil {
		return 0, true
	}
	limits, err := p.limits.Limits(ctx, organizationID)
	if err != nil {
		p.logger.Warn("read customer alert budget failed",
			"organization_id", organizationID, "error", err)
		return 0, false
	}
	// The stored value is clamped to the current maximum on the way out.
	return clampNonNegativeUint32(min(limits.DeviceHourly, alerts.MaxDeviceHourlyCeiling)), true
}
