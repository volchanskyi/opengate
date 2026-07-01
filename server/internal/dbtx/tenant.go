// Package dbtx carries tenant scope and helpers for Postgres RLS transactions.
package dbtx

import (
	"context"

	"github.com/google/uuid"
)

// DefaultOrgID is the seeded single-tenant organization used during migration
// and by legacy single-org token generation.
var DefaultOrgID = uuid.MustParse("00000000-0000-0000-0000-000000000002")

type tenantKey struct{}

// Tenant describes the current request's organization scope.
type Tenant struct {
	// OrgID is the organization scope for tenant tables.
	OrgID uuid.UUID
	// IsAdmin permits policy-based cross-org reads when explicitly set in RLS.
	IsAdmin bool
}

// WithTenant stores tenant scope on ctx.
func WithTenant(ctx context.Context, orgID uuid.UUID, isAdmin bool) context.Context {
	return context.WithValue(ctx, tenantKey{}, Tenant{OrgID: orgID, IsAdmin: isAdmin})
}

// WithDefaultTenant stores the seeded default organization on ctx.
func WithDefaultTenant(ctx context.Context, isAdmin bool) context.Context {
	return WithTenant(ctx, DefaultOrgID, isAdmin)
}

// TenantFromContext returns tenant scope from ctx.
func TenantFromContext(ctx context.Context) (Tenant, bool) {
	tenant, ok := ctx.Value(tenantKey{}).(Tenant)
	if !ok || tenant.OrgID == uuid.Nil {
		return Tenant{}, false
	}
	return tenant, true
}
