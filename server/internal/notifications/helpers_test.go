package notifications

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
)

// testVAPIDKeys generates a valid VAPID key pair for use in tests.
func testVAPIDKeys(t *testing.T) (priv, pub string) {
	t.Helper()
	dir := t.TempDir()
	priv, pub, err := LoadOrGenerateVAPID(dir)
	require.NoError(t, err)
	return priv, pub
}

// mockSub is a test helper for creating push subscriptions.
type mockSub struct {
	Endpoint string
	UserID   uuid.UUID
	P256dh   string
	Auth     string
}

// notifMockStore implements only the db.Store methods used by PushNotifier.
type notifMockStore struct {
	subs       []*db.WebPushSubscription
	deletedEPs []string
	mu         sync.Mutex
}

func newMockNotifStore(subs []*mockSub) *notifMockStore {
	var dbSubs []*db.WebPushSubscription
	for _, s := range subs {
		dbSubs = append(dbSubs, &db.WebPushSubscription{
			Endpoint: s.Endpoint,
			UserID:   s.UserID,
			P256dh:   s.P256dh,
			Auth:     s.Auth,
		})
	}
	return &notifMockStore{subs: dbSubs}
}

func (m *notifMockStore) ListAllWebPushSubscriptions(_ context.Context) ([]*db.WebPushSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.subs, nil
}

func (m *notifMockStore) DeleteWebPushSubscription(_ context.Context, endpoint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedEPs = append(m.deletedEPs, endpoint)
	return nil
}

// Unused Store methods — stubs to satisfy the interface.
func (m *notifMockStore) UpsertDevice(_ context.Context, _ *db.Device) error { return nil }
func (m *notifMockStore) GetDevice(_ context.Context, _ db.DeviceID) (*db.Device, error) {
	return nil, nil
}
func (m *notifMockStore) ListDevices(_ context.Context, _ db.GroupID) ([]*db.Device, error) {
	return nil, nil
}
func (m *notifMockStore) ListAllDevices(_ context.Context) ([]*db.Device, error) {
	return nil, nil
}
func (m *notifMockStore) ListDevicesForOwner(_ context.Context, _ db.UserID) ([]*db.Device, error) {
	return nil, nil
}
func (m *notifMockStore) DeleteDevice(_ context.Context, _ db.DeviceID) error { return nil }
func (m *notifMockStore) UpdateDeviceGroup(_ context.Context, _ db.DeviceID, _ db.GroupID) error {
	return nil
}
func (m *notifMockStore) SetDeviceStatus(_ context.Context, _ db.DeviceID, _ db.DeviceStatus) error {
	return nil
}
func (m *notifMockStore) ResetAllDeviceStatuses(_ context.Context) error { return nil }
func (m *notifMockStore) CreateGroup(_ context.Context, _ *db.Group) error { return nil }
func (m *notifMockStore) GetGroup(_ context.Context, _ db.GroupID) (*db.Group, error) {
	return nil, nil
}
func (m *notifMockStore) ListGroups(_ context.Context, _ db.UserID) ([]*db.Group, error) {
	return nil, nil
}
func (m *notifMockStore) DeleteGroup(_ context.Context, _ db.GroupID) error { return nil }
func (m *notifMockStore) UpsertUser(_ context.Context, _ *db.User) error    { return nil }
func (m *notifMockStore) GetUser(_ context.Context, _ db.UserID) (*db.User, error) {
	return nil, nil
}
func (m *notifMockStore) GetUserByEmail(_ context.Context, _ string) (*db.User, error) {
	return nil, nil
}
func (m *notifMockStore) ListUsers(_ context.Context) ([]*db.User, error) { return nil, nil }
func (m *notifMockStore) DeleteUser(_ context.Context, _ db.UserID) error  { return nil }
func (m *notifMockStore) CreateAgentSession(_ context.Context, _ *db.AgentSession) error {
	return nil
}
func (m *notifMockStore) GetAgentSession(_ context.Context, _ string) (*db.AgentSession, error) {
	return nil, nil
}
func (m *notifMockStore) DeleteAgentSession(_ context.Context, _ string) error { return nil }
func (m *notifMockStore) ListActiveSessionsForDevice(_ context.Context, _ db.DeviceID) ([]*db.AgentSession, error) {
	return nil, nil
}
func (m *notifMockStore) UpsertWebPushSubscription(_ context.Context, _ *db.WebPushSubscription) error {
	return nil
}
func (m *notifMockStore) ListWebPushSubscriptions(_ context.Context, _ uuid.UUID) ([]*db.WebPushSubscription, error) {
	return nil, nil
}
func (m *notifMockStore) WriteAuditEvent(_ context.Context, _ *db.AuditEvent) error { return nil }
func (m *notifMockStore) QueryAuditLog(_ context.Context, _ db.AuditQuery) ([]*db.AuditEvent, error) {
	return nil, nil
}
func (m *notifMockStore) UpsertAMTDevice(_ context.Context, _ *db.AMTDevice) error { return nil }
func (m *notifMockStore) GetAMTDevice(_ context.Context, _ uuid.UUID) (*db.AMTDevice, error) {
	return nil, nil
}
func (m *notifMockStore) ListAMTDevices(_ context.Context) ([]*db.AMTDevice, error) {
	return nil, nil
}
func (m *notifMockStore) SetAMTDeviceStatus(_ context.Context, _ uuid.UUID, _ db.DeviceStatus) error {
	return nil
}
func (m *notifMockStore) CreateEnrollmentToken(_ context.Context, _ *db.EnrollmentToken) error {
	return nil
}
func (m *notifMockStore) GetEnrollmentTokenByToken(_ context.Context, _ string) (*db.EnrollmentToken, error) {
	return nil, nil
}
func (m *notifMockStore) ListEnrollmentTokens(_ context.Context, _ db.UserID) ([]*db.EnrollmentToken, error) {
	return nil, nil
}
func (m *notifMockStore) DeleteEnrollmentToken(_ context.Context, _ uuid.UUID) error { return nil }
func (m *notifMockStore) IncrementEnrollmentTokenUseCount(_ context.Context, _ uuid.UUID) error {
	return nil
}
func (m *notifMockStore) CreateSecurityGroup(_ context.Context, _ *db.SecurityGroup) error {
	return nil
}
func (m *notifMockStore) GetSecurityGroup(_ context.Context, _ db.SecurityGroupID) (*db.SecurityGroup, error) {
	return nil, db.ErrNotFound
}
func (m *notifMockStore) ListSecurityGroups(_ context.Context) ([]*db.SecurityGroup, error) {
	return nil, nil
}
func (m *notifMockStore) DeleteSecurityGroup(_ context.Context, _ db.SecurityGroupID) error {
	return nil
}
func (m *notifMockStore) AddSecurityGroupMember(_ context.Context, _ db.SecurityGroupID, _ db.UserID) error {
	return nil
}
func (m *notifMockStore) RemoveSecurityGroupMember(_ context.Context, _ db.SecurityGroupID, _ db.UserID) error {
	return nil
}
func (m *notifMockStore) ListSecurityGroupMembers(_ context.Context, _ db.SecurityGroupID) ([]*db.User, error) {
	return nil, nil
}
func (m *notifMockStore) IsUserInSecurityGroup(_ context.Context, _ db.UserID, _ db.SecurityGroupID) (bool, error) {
	return false, nil
}
func (m *notifMockStore) CountSecurityGroupMembers(_ context.Context, _ db.SecurityGroupID) (int, error) {
	return 0, nil
}
func (m *notifMockStore) CreateDeviceUpdate(_ context.Context, _ *db.DeviceUpdate) error {
	return nil
}
func (m *notifMockStore) UpdateDeviceUpdateStatus(_ context.Context, _ db.DeviceID, _ string, _ db.UpdateStatus, _ string) error {
	return nil
}
func (m *notifMockStore) ListDeviceUpdatesByVersion(_ context.Context, _ string) ([]*db.DeviceUpdate, error) {
	return nil, nil
}
func (m *notifMockStore) UpsertDeviceHardware(_ context.Context, _ *db.DeviceHardware) error {
	return nil
}
func (m *notifMockStore) GetDeviceHardware(_ context.Context, _ db.DeviceID) (*db.DeviceHardware, error) {
	return nil, db.ErrNotFound
}
func (m *notifMockStore) UpsertDeviceLogs(_ context.Context, _ db.DeviceID, _ []db.DeviceLogEntry) error {
	return nil
}
func (m *notifMockStore) QueryDeviceLogs(_ context.Context, _ db.DeviceID, _ db.LogFilter) ([]db.DeviceLogEntry, int, error) {
	return nil, 0, nil
}
func (m *notifMockStore) HasRecentLogs(_ context.Context, _ db.DeviceID, _ time.Duration) (bool, error) {
	return false, nil
}
func (m *notifMockStore) Ping(_ context.Context) error { return nil }
func (m *notifMockStore) Close() error                 { return nil }

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
