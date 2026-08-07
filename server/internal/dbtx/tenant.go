// Package dbtx carries tenant scope and helpers for Postgres RLS transactions.
package dbtx

import (
	"context"

	"github.com/google/uuid"
)

// DefaultTenantID is the seeded single-tenant tenant used during migration
// and by legacy single-tenant token generation.
var DefaultTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000002")

type tenantKey struct{}

// Tenant describes the current request's tenant scope.
type Tenant struct {
	// TenantID is the tenant scope for tenant tables.
	TenantID uuid.UUID
	// IsAdmin permits policy-based cross-tenant reads when explicitly set in RLS.
	IsAdmin bool
}

// WithTenant stores tenant scope on ctx.
func WithTenant(ctx context.Context, tenantID uuid.UUID, isAdmin bool) context.Context {
	return context.WithValue(ctx, tenantKey{}, Tenant{TenantID: tenantID, IsAdmin: isAdmin})
}

// WithDefaultTenant stores the seeded default tenant on ctx.
func WithDefaultTenant(ctx context.Context, isAdmin bool) context.Context {
	return WithTenant(ctx, DefaultTenantID, isAdmin)
}

// TenantFromContext returns tenant scope from ctx.
func TenantFromContext(ctx context.Context) (Tenant, bool) {
	tenant, ok := ctx.Value(tenantKey{}).(Tenant)
	if !ok || tenant.TenantID == uuid.Nil {
		return Tenant{}, false
	}
	return tenant, true
}
