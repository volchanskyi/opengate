package api

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

type auditContextKey struct{}

type contextCapturingAuditRepo struct {
	contexts chan context.Context
}

func (r *contextCapturingAuditRepo) Write(ctx context.Context, _ *audit.Event) error {
	r.contexts <- ctx
	return nil
}

func (*contextCapturingAuditRepo) Query(context.Context, audit.Query) ([]*audit.Event, error) {
	return nil, nil
}

func TestAuditLogPreservesRequestValuesAfterCancellation(t *testing.T) {
	t.Parallel()

	repo := &contextCapturingAuditRepo{contexts: make(chan context.Context, 1)}
	srv := &Server{audit: repo, logger: slog.Default()}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), auditContextKey{}, "request-value"))
	ctx = dbtx.WithDefaultTenant(ctx, true)
	cancel()

	srv.auditLog(ctx, uuid.New(), "device.delete", "device-id", "right-to-be-forgotten purge")

	var auditCtx context.Context
	require.Eventually(t, func() bool {
		select {
		case auditCtx = <-repo.contexts:
			return true
		default:
			return false
		}
	}, 2*time.Second, 10*time.Millisecond, "the audit write should run asynchronously")
	assert.Equal(t, "request-value", auditCtx.Value(auditContextKey{}))
	tenant, ok := dbtx.TenantFromContext(auditCtx)
	require.True(t, ok)
	assert.True(t, tenant.IsAdmin)
}
