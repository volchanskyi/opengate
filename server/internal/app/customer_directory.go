package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/organization"
)

// customerDirectory lists the customers whose rule settings the agent server holds in memory.
type customerDirectory struct {
	organizations organization.Repository
}

func (d customerDirectory) AllCustomers(ctx context.Context) ([]agentapi.CustomerRef, error) {
	refs, err := d.organizations.AllCustomers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]agentapi.CustomerRef, len(refs))
	for i, r := range refs {
		out[i] = agentapi.CustomerRef{TenantID: r.TenantID, OrganizationID: r.ID}
	}
	return out, nil
}

func (d customerDirectory) CustomersIn(ctx context.Context) ([]uuid.UUID, error) {
	orgs, err := d.organizations.List(ctx, true)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, len(orgs))
	for i, o := range orgs {
		out[i] = o.ID
	}
	return out, nil
}

// tenants lists every tenant holding a customer, which is every tenant holding a machine, since a
// machine always belongs to a customer.
func (d customerDirectory) tenants(ctx context.Context) ([]uuid.UUID, error) {
	customers, err := d.AllCustomers(dbtx.WithDefaultTenant(ctx, true))
	if err != nil {
		return nil, err
	}
	seen := make(map[uuid.UUID]bool, len(customers))
	var tenants []uuid.UUID
	for _, c := range customers {
		if !seen[c.TenantID] {
			seen[c.TenantID] = true
			tenants = append(tenants, c.TenantID)
		}
	}
	return tenants, nil
}
