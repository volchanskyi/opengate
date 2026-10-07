package api

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

func TestDeliverRuleChangeReachesTheScopeItWasMadeFor(t *testing.T) {
	t.Parallel()

	org, tenant := uuid.New(), uuid.New()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name        string
		ctx         context.Context
		tenantWide  bool
		wantOrgs    []uuid.UUID
		wantTenants []uuid.UUID
	}{
		{
			name:     "a customer's change reaches that customer's machines",
			ctx:      context.Background(),
			wantOrgs: []uuid.UUID{org},
		},
		{
			name:        "a tenant-wide stop reaches every customer in the tenant",
			ctx:         dbtx.WithTenant(context.Background(), tenant, true),
			tenantWide:  true,
			wantTenants: []uuid.UUID{tenant},
		},
		{
			// A request with no tenant has no scope to deliver to.
			name:       "a tenant-wide stop with no tenant on the request reaches nobody",
			ctx:        context.Background(),
			tenantWide: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			agents := &stubAgentGetter{}
			s := &Server{agents: agents, logger: quiet}

			s.deliverRuleChange(tc.ctx, org, tc.tenantWide)

			assert.Equal(t, tc.wantOrgs, agents.refreshedFor)
			assert.Equal(t, tc.wantTenants, agents.refreshedTenants)
		})
	}
}

func TestDeliverRuleChangeWithNoAgentServerDeliversNothing(t *testing.T) {
	t.Parallel()

	s := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	assert.NotPanics(t, func() { s.deliverRuleChange(context.Background(), uuid.New(), true) })
}
