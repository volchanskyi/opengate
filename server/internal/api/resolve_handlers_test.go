package api

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/amt/transport/wsman"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/notifications"
)

// These tests exercise every branch of every resolve<Domain>Handlers fallback
// per ADR-020 §9 / plan §4.1. The fallbacks let existing test ServerConfig
// literals stay green (auto-wrap legacy Repository/Operator/Notifier into the
// new Handlers struct) while main.go and new test code pass *.Handlers
// explicitly. Each function has three branches that must be covered for
// SonarCloud new_coverage to clear 80% on the audit-pilot diff.

// ---- audit ----

type minimalAuditRepo struct{}

func (minimalAuditRepo) Write(context.Context, *audit.Event) error { return nil }
func (minimalAuditRepo) Query(context.Context, audit.Query) ([]*audit.Event, error) {
	return nil, nil
}

func TestResolveAuditHandlers_PrefersExplicitHandlers(t *testing.T) {
	explicit := audit.NewHandlers(minimalAuditRepo{})
	got := resolveAuditHandlers(ServerConfig{AuditHandlers: explicit, Audit: minimalAuditRepo{}})
	require.Same(t, explicit, got, "explicit AuditHandlers must win over the Audit fallback")
}

func TestResolveAuditHandlers_FallsBackToRepository(t *testing.T) {
	repo := minimalAuditRepo{}
	got := resolveAuditHandlers(ServerConfig{Audit: repo})
	require.NotNil(t, got, "fallback must wrap a non-nil Audit Repository")
}

func TestResolveAuditHandlers_NilWhenBothMissing(t *testing.T) {
	require.Nil(t, resolveAuditHandlers(ServerConfig{}))
}

// ---- amt ----

type minimalAMTRepo struct{}

func (minimalAMTRepo) Upsert(context.Context, *db.AMTDevice) error { return nil }
func (minimalAMTRepo) Get(context.Context, uuid.UUID) (*db.AMTDevice, error) {
	return nil, nil
}
func (minimalAMTRepo) List(context.Context) ([]*db.AMTDevice, error) { return nil, nil }
func (minimalAMTRepo) SetStatus(context.Context, uuid.UUID, db.DeviceStatus) error {
	return nil
}

type minimalAMTOperator struct{}

func (minimalAMTOperator) PowerAction(context.Context, uuid.UUID, int) error { return nil }
func (minimalAMTOperator) QueryDeviceInfo(context.Context, uuid.UUID) (*wsman.DeviceInfo, error) {
	return nil, nil
}
func (minimalAMTOperator) ConnectedDeviceCount() int { return 0 }

func TestResolveAMTHandlers_PrefersExplicitHandlers(t *testing.T) {
	explicit := amt.NewHandlers(minimalAMTRepo{}, minimalAMTOperator{})
	got := resolveAMTHandlers(ServerConfig{
		AMTHandlers: explicit,
		AMTDevices:  minimalAMTRepo{},
		AMT:         minimalAMTOperator{},
	})
	require.Same(t, explicit, got)
}

func TestResolveAMTHandlers_FallsBackWhenBothPortsPresent(t *testing.T) {
	got := resolveAMTHandlers(ServerConfig{
		AMTDevices: minimalAMTRepo{},
		AMT:        minimalAMTOperator{},
	})
	require.NotNil(t, got, "fallback must wrap when both Repository and Operator are set")
}

func TestResolveAMTHandlers_NilWhenAnyPortMissing(t *testing.T) {
	// Only Repository, no Operator → must NOT construct (Handlers needs both).
	require.Nil(t, resolveAMTHandlers(ServerConfig{AMTDevices: minimalAMTRepo{}}))
	// Only Operator, no Repository → likewise.
	require.Nil(t, resolveAMTHandlers(ServerConfig{AMT: minimalAMTOperator{}}))
	// Neither.
	require.Nil(t, resolveAMTHandlers(ServerConfig{}))
}

// ---- notifications ----

type minimalWebPush struct{}

func (minimalWebPush) Upsert(context.Context, *notifications.WebPushSubscription) error {
	return nil
}
func (minimalWebPush) ListForUser(context.Context, uuid.UUID) ([]*notifications.WebPushSubscription, error) {
	return nil, nil
}
func (minimalWebPush) ListAll(context.Context) ([]*notifications.WebPushSubscription, error) {
	return nil, nil
}
func (minimalWebPush) Delete(context.Context, string) error { return nil }

type minimalNotifier struct{}

func (minimalNotifier) Notify(context.Context, notifications.Event) error { return nil }
func (minimalNotifier) VAPIDPublicKey() string                            { return "" }

func TestResolveNotificationsHandlers_PrefersExplicitHandlers(t *testing.T) {
	explicit := notifications.NewHandlers(minimalWebPush{}, minimalNotifier{})
	got := resolveNotificationsHandlers(ServerConfig{
		NotificationsHandlers: explicit,
		WebPush:               minimalWebPush{},
		Notifier:              minimalNotifier{},
	})
	require.Same(t, explicit, got)
}

func TestResolveNotificationsHandlers_FallsBackWhenBothPortsPresent(t *testing.T) {
	got := resolveNotificationsHandlers(ServerConfig{
		WebPush:  minimalWebPush{},
		Notifier: minimalNotifier{},
	})
	require.NotNil(t, got)
}

func TestResolveNotificationsHandlers_NilWhenAnyPortMissing(t *testing.T) {
	require.Nil(t, resolveNotificationsHandlers(ServerConfig{WebPush: minimalWebPush{}}))
	require.Nil(t, resolveNotificationsHandlers(ServerConfig{Notifier: minimalNotifier{}}))
	require.Nil(t, resolveNotificationsHandlers(ServerConfig{}))
}
