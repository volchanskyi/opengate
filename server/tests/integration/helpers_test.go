package integration

import (
	"context"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

func defaultTenantContext() context.Context {
	return dbtx.WithDefaultTenant(context.Background(), false)
}
