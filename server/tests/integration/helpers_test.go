package integration

import (
	"context"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// This tier holds what needs a transport: a real QUIC peer, a real socket, or a
// WebSocket. Everything else belongs beside the code it exercises — the rule is
// enforced by scripts/tests/test-tier-placement.test.sh.

// defaultTenantContext is the tenant scope a test arranges its fixtures in.
func defaultTenantContext() context.Context {
	return dbtx.WithDefaultTenant(context.Background(), false)
}
