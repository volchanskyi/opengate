package metrics

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/db"
)

// InstrumentedStore wraps a db.Store and records query duration and count
// metrics for every operation.
type InstrumentedStore struct {
	inner   db.Store
	metrics *Metrics
}

// NewInstrumentedStore wraps the given store with Prometheus instrumentation.
func NewInstrumentedStore(store db.Store, m *Metrics) *InstrumentedStore {
	return &InstrumentedStore{inner: store, metrics: m}
}

func (s *InstrumentedStore) observe(operation string, start time.Time, err error) {
	duration := time.Since(start).Seconds()
	status := "ok"
	if err != nil {
		status = "error"
	}
	s.metrics.DBQueryDuration.WithLabelValues(operation).Observe(duration)
	s.metrics.DBQueriesTotal.WithLabelValues(operation, status).Inc()
}

// --- Devices -----------------------------------------------------------------

// UpsertDevice instruments db.Store.UpsertDevice.
func (s *InstrumentedStore) UpsertDevice(ctx context.Context, d *db.Device) error {
	start := time.Now()
	err := s.inner.UpsertDevice(ctx, d)
	s.observe("UpsertDevice", start, err)
	return err
}

// GetDevice instruments db.Store.GetDevice.
func (s *InstrumentedStore) GetDevice(ctx context.Context, id db.DeviceID) (*db.Device, error) {
	start := time.Now()
	d, err := s.inner.GetDevice(ctx, id)
	s.observe("GetDevice", start, err)
	return d, err
}

// ListDevices instruments db.Store.ListDevices.
func (s *InstrumentedStore) ListDevices(ctx context.Context, groupID db.GroupID) ([]*db.Device, error) {
	start := time.Now()
	d, err := s.inner.ListDevices(ctx, groupID)
	s.observe("ListDevices", start, err)
	return d, err
}

// ListAllDevices instruments db.Store.ListAllDevices.
func (s *InstrumentedStore) ListAllDevices(ctx context.Context) ([]*db.Device, error) {
	start := time.Now()
	d, err := s.inner.ListAllDevices(ctx)
	s.observe("ListAllDevices", start, err)
	return d, err
}

// ListDevicesForOwner instruments db.Store.ListDevicesForOwner.
func (s *InstrumentedStore) ListDevicesForOwner(ctx context.Context, ownerID db.UserID) ([]*db.Device, error) {
	start := time.Now()
	d, err := s.inner.ListDevicesForOwner(ctx, ownerID)
	s.observe("ListDevicesForOwner", start, err)
	return d, err
}

// DeleteDevice instruments db.Store.DeleteDevice.
func (s *InstrumentedStore) DeleteDevice(ctx context.Context, id db.DeviceID) error {
	start := time.Now()
	err := s.inner.DeleteDevice(ctx, id)
	s.observe("DeleteDevice", start, err)
	return err
}

// UpdateDeviceGroup instruments db.Store.UpdateDeviceGroup.
func (s *InstrumentedStore) UpdateDeviceGroup(ctx context.Context, id db.DeviceID, groupID db.GroupID) error {
	start := time.Now()
	err := s.inner.UpdateDeviceGroup(ctx, id, groupID)
	s.observe("UpdateDeviceGroup", start, err)
	return err
}

// SetDeviceStatus instruments db.Store.SetDeviceStatus.
func (s *InstrumentedStore) SetDeviceStatus(ctx context.Context, id db.DeviceID, status db.DeviceStatus) error {
	start := time.Now()
	err := s.inner.SetDeviceStatus(ctx, id, status)
	s.observe("SetDeviceStatus", start, err)
	return err
}

// --- Groups ------------------------------------------------------------------

// CreateGroup instruments db.Store.CreateGroup.
func (s *InstrumentedStore) CreateGroup(ctx context.Context, g *db.Group) error {
	start := time.Now()
	err := s.inner.CreateGroup(ctx, g)
	s.observe("CreateGroup", start, err)
	return err
}

// GetGroup instruments db.Store.GetGroup.
func (s *InstrumentedStore) GetGroup(ctx context.Context, id db.GroupID) (*db.Group, error) {
	start := time.Now()
	g, err := s.inner.GetGroup(ctx, id)
	s.observe("GetGroup", start, err)
	return g, err
}

// ListGroups instruments db.Store.ListGroups.
func (s *InstrumentedStore) ListGroups(ctx context.Context, ownerID db.UserID) ([]*db.Group, error) {
	start := time.Now()
	g, err := s.inner.ListGroups(ctx, ownerID)
	s.observe("ListGroups", start, err)
	return g, err
}

// DeleteGroup instruments db.Store.DeleteGroup.
func (s *InstrumentedStore) DeleteGroup(ctx context.Context, id db.GroupID) error {
	start := time.Now()
	err := s.inner.DeleteGroup(ctx, id)
	s.observe("DeleteGroup", start, err)
	return err
}

// --- Users -------------------------------------------------------------------

// UpsertUser instruments db.Store.UpsertUser.
func (s *InstrumentedStore) UpsertUser(ctx context.Context, u *db.User) error {
	start := time.Now()
	err := s.inner.UpsertUser(ctx, u)
	s.observe("UpsertUser", start, err)
	return err
}

// GetUser instruments db.Store.GetUser.
func (s *InstrumentedStore) GetUser(ctx context.Context, id db.UserID) (*db.User, error) {
	start := time.Now()
	u, err := s.inner.GetUser(ctx, id)
	s.observe("GetUser", start, err)
	return u, err
}

// GetUserByEmail instruments db.Store.GetUserByEmail.
func (s *InstrumentedStore) GetUserByEmail(ctx context.Context, email string) (*db.User, error) {
	start := time.Now()
	u, err := s.inner.GetUserByEmail(ctx, email)
	s.observe("GetUserByEmail", start, err)
	return u, err
}

// ListUsers instruments db.Store.ListUsers.
func (s *InstrumentedStore) ListUsers(ctx context.Context) ([]*db.User, error) {
	start := time.Now()
	u, err := s.inner.ListUsers(ctx)
	s.observe("ListUsers", start, err)
	return u, err
}

// DeleteUser instruments db.Store.DeleteUser.
func (s *InstrumentedStore) DeleteUser(ctx context.Context, id db.UserID) error {
	start := time.Now()
	err := s.inner.DeleteUser(ctx, id)
	s.observe("DeleteUser", start, err)
	return err
}

// --- Agent Sessions ----------------------------------------------------------

// CreateAgentSession instruments db.Store.CreateAgentSession.
func (s *InstrumentedStore) CreateAgentSession(ctx context.Context, sess *db.AgentSession) error {
	start := time.Now()
	err := s.inner.CreateAgentSession(ctx, sess)
	s.observe("CreateAgentSession", start, err)
	return err
}

// GetAgentSession instruments db.Store.GetAgentSession.
func (s *InstrumentedStore) GetAgentSession(ctx context.Context, token string) (*db.AgentSession, error) {
	start := time.Now()
	sess, err := s.inner.GetAgentSession(ctx, token)
	s.observe("GetAgentSession", start, err)
	return sess, err
}

// DeleteAgentSession instruments db.Store.DeleteAgentSession.
func (s *InstrumentedStore) DeleteAgentSession(ctx context.Context, token string) error {
	start := time.Now()
	err := s.inner.DeleteAgentSession(ctx, token)
	s.observe("DeleteAgentSession", start, err)
	return err
}

// ListActiveSessionsForDevice instruments db.Store.ListActiveSessionsForDevice.
func (s *InstrumentedStore) ListActiveSessionsForDevice(ctx context.Context, deviceID db.DeviceID) ([]*db.AgentSession, error) {
	start := time.Now()
	sess, err := s.inner.ListActiveSessionsForDevice(ctx, deviceID)
	s.observe("ListActiveSessionsForDevice", start, err)
	return sess, err
}

// --- Web Push ----------------------------------------------------------------

// UpsertWebPushSubscription instruments db.Store.UpsertWebPushSubscription.
func (s *InstrumentedStore) UpsertWebPushSubscription(ctx context.Context, sub *db.WebPushSubscription) error {
	start := time.Now()
	err := s.inner.UpsertWebPushSubscription(ctx, sub)
	s.observe("UpsertWebPushSubscription", start, err)
	return err
}

// ListWebPushSubscriptions instruments db.Store.ListWebPushSubscriptions.
func (s *InstrumentedStore) ListWebPushSubscriptions(ctx context.Context, userID db.UserID) ([]*db.WebPushSubscription, error) {
	start := time.Now()
	subs, err := s.inner.ListWebPushSubscriptions(ctx, userID)
	s.observe("ListWebPushSubscriptions", start, err)
	return subs, err
}

// ListAllWebPushSubscriptions instruments db.Store.ListAllWebPushSubscriptions.
func (s *InstrumentedStore) ListAllWebPushSubscriptions(ctx context.Context) ([]*db.WebPushSubscription, error) {
	start := time.Now()
	subs, err := s.inner.ListAllWebPushSubscriptions(ctx)
	s.observe("ListAllWebPushSubscriptions", start, err)
	return subs, err
}

// DeleteWebPushSubscription instruments db.Store.DeleteWebPushSubscription.
func (s *InstrumentedStore) DeleteWebPushSubscription(ctx context.Context, endpoint string) error {
	start := time.Now()
	err := s.inner.DeleteWebPushSubscription(ctx, endpoint)
	s.observe("DeleteWebPushSubscription", start, err)
	return err
}

// --- AMT Devices -------------------------------------------------------------

// UpsertAMTDevice instruments db.Store.UpsertAMTDevice.
func (s *InstrumentedStore) UpsertAMTDevice(ctx context.Context, d *db.AMTDevice) error {
	start := time.Now()
	err := s.inner.UpsertAMTDevice(ctx, d)
	s.observe("UpsertAMTDevice", start, err)
	return err
}

// GetAMTDevice instruments db.Store.GetAMTDevice.
func (s *InstrumentedStore) GetAMTDevice(ctx context.Context, id uuid.UUID) (*db.AMTDevice, error) {
	start := time.Now()
	d, err := s.inner.GetAMTDevice(ctx, id)
	s.observe("GetAMTDevice", start, err)
	return d, err
}

// ListAMTDevices instruments db.Store.ListAMTDevices.
func (s *InstrumentedStore) ListAMTDevices(ctx context.Context) ([]*db.AMTDevice, error) {
	start := time.Now()
	d, err := s.inner.ListAMTDevices(ctx)
	s.observe("ListAMTDevices", start, err)
	return d, err
}

// SetAMTDeviceStatus instruments db.Store.SetAMTDeviceStatus.
func (s *InstrumentedStore) SetAMTDeviceStatus(ctx context.Context, id uuid.UUID, status db.DeviceStatus) error {
	start := time.Now()
	err := s.inner.SetAMTDeviceStatus(ctx, id, status)
	s.observe("SetAMTDeviceStatus", start, err)
	return err
}

// --- Enrollment Tokens -------------------------------------------------------

// CreateEnrollmentToken instruments db.Store.CreateEnrollmentToken.
func (s *InstrumentedStore) CreateEnrollmentToken(ctx context.Context, t *db.EnrollmentToken) error {
	start := time.Now()
	err := s.inner.CreateEnrollmentToken(ctx, t)
	s.observe("CreateEnrollmentToken", start, err)
	return err
}

// GetEnrollmentTokenByToken instruments db.Store.GetEnrollmentTokenByToken.
func (s *InstrumentedStore) GetEnrollmentTokenByToken(ctx context.Context, token string) (*db.EnrollmentToken, error) {
	start := time.Now()
	t, err := s.inner.GetEnrollmentTokenByToken(ctx, token)
	s.observe("GetEnrollmentTokenByToken", start, err)
	return t, err
}

// ListEnrollmentTokens instruments db.Store.ListEnrollmentTokens.
func (s *InstrumentedStore) ListEnrollmentTokens(ctx context.Context, createdBy db.UserID) ([]*db.EnrollmentToken, error) {
	start := time.Now()
	t, err := s.inner.ListEnrollmentTokens(ctx, createdBy)
	s.observe("ListEnrollmentTokens", start, err)
	return t, err
}

// DeleteEnrollmentToken instruments db.Store.DeleteEnrollmentToken.
func (s *InstrumentedStore) DeleteEnrollmentToken(ctx context.Context, id uuid.UUID) error {
	start := time.Now()
	err := s.inner.DeleteEnrollmentToken(ctx, id)
	s.observe("DeleteEnrollmentToken", start, err)
	return err
}

// IncrementEnrollmentTokenUseCount instruments db.Store.IncrementEnrollmentTokenUseCount.
func (s *InstrumentedStore) IncrementEnrollmentTokenUseCount(ctx context.Context, id uuid.UUID) error {
	start := time.Now()
	err := s.inner.IncrementEnrollmentTokenUseCount(ctx, id)
	s.observe("IncrementEnrollmentTokenUseCount", start, err)
	return err
}

// --- Audit -------------------------------------------------------------------

// WriteAuditEvent instruments db.Store.WriteAuditEvent.
func (s *InstrumentedStore) WriteAuditEvent(ctx context.Context, event *db.AuditEvent) error {
	start := time.Now()
	err := s.inner.WriteAuditEvent(ctx, event)
	s.observe("WriteAuditEvent", start, err)
	return err
}

// QueryAuditLog instruments db.Store.QueryAuditLog.
func (s *InstrumentedStore) QueryAuditLog(ctx context.Context, q db.AuditQuery) ([]*db.AuditEvent, error) {
	start := time.Now()
	events, err := s.inner.QueryAuditLog(ctx, q)
	s.observe("QueryAuditLog", start, err)
	return events, err
}

// --- Device Updates ----------------------------------------------------------

// CreateDeviceUpdate instruments db.Store.CreateDeviceUpdate.
func (s *InstrumentedStore) CreateDeviceUpdate(ctx context.Context, du *db.DeviceUpdate) error {
	start := time.Now()
	err := s.inner.CreateDeviceUpdate(ctx, du)
	s.observe("CreateDeviceUpdate", start, err)
	return err
}

// UpdateDeviceUpdateStatus instruments db.Store.UpdateDeviceUpdateStatus.
func (s *InstrumentedStore) UpdateDeviceUpdateStatus(ctx context.Context, deviceID db.DeviceID, version string, status db.UpdateStatus, errMsg string) error {
	start := time.Now()
	err := s.inner.UpdateDeviceUpdateStatus(ctx, deviceID, version, status, errMsg)
	s.observe("UpdateDeviceUpdateStatus", start, err)
	return err
}

// ListDeviceUpdatesByVersion instruments db.Store.ListDeviceUpdatesByVersion.
func (s *InstrumentedStore) ListDeviceUpdatesByVersion(ctx context.Context, version string) ([]*db.DeviceUpdate, error) {
	start := time.Now()
	du, err := s.inner.ListDeviceUpdatesByVersion(ctx, version)
	s.observe("ListDeviceUpdatesByVersion", start, err)
	return du, err
}

// --- Security Groups ---------------------------------------------------------

// CreateSecurityGroup instruments db.Store.CreateSecurityGroup.
func (s *InstrumentedStore) CreateSecurityGroup(ctx context.Context, g *db.SecurityGroup) error {
	start := time.Now()
	err := s.inner.CreateSecurityGroup(ctx, g)
	s.observe("CreateSecurityGroup", start, err)
	return err
}

// GetSecurityGroup instruments db.Store.GetSecurityGroup.
func (s *InstrumentedStore) GetSecurityGroup(ctx context.Context, id db.SecurityGroupID) (*db.SecurityGroup, error) {
	start := time.Now()
	g, err := s.inner.GetSecurityGroup(ctx, id)
	s.observe("GetSecurityGroup", start, err)
	return g, err
}

// ListSecurityGroups instruments db.Store.ListSecurityGroups.
func (s *InstrumentedStore) ListSecurityGroups(ctx context.Context) ([]*db.SecurityGroup, error) {
	start := time.Now()
	g, err := s.inner.ListSecurityGroups(ctx)
	s.observe("ListSecurityGroups", start, err)
	return g, err
}

// DeleteSecurityGroup instruments db.Store.DeleteSecurityGroup.
func (s *InstrumentedStore) DeleteSecurityGroup(ctx context.Context, id db.SecurityGroupID) error {
	start := time.Now()
	err := s.inner.DeleteSecurityGroup(ctx, id)
	s.observe("DeleteSecurityGroup", start, err)
	return err
}

// AddSecurityGroupMember instruments db.Store.AddSecurityGroupMember.
func (s *InstrumentedStore) AddSecurityGroupMember(ctx context.Context, groupID db.SecurityGroupID, userID db.UserID) error {
	start := time.Now()
	err := s.inner.AddSecurityGroupMember(ctx, groupID, userID)
	s.observe("AddSecurityGroupMember", start, err)
	return err
}

// RemoveSecurityGroupMember instruments db.Store.RemoveSecurityGroupMember.
func (s *InstrumentedStore) RemoveSecurityGroupMember(ctx context.Context, groupID db.SecurityGroupID, userID db.UserID) error {
	start := time.Now()
	err := s.inner.RemoveSecurityGroupMember(ctx, groupID, userID)
	s.observe("RemoveSecurityGroupMember", start, err)
	return err
}

// ListSecurityGroupMembers instruments db.Store.ListSecurityGroupMembers.
func (s *InstrumentedStore) ListSecurityGroupMembers(ctx context.Context, groupID db.SecurityGroupID) ([]*db.User, error) {
	start := time.Now()
	u, err := s.inner.ListSecurityGroupMembers(ctx, groupID)
	s.observe("ListSecurityGroupMembers", start, err)
	return u, err
}

// IsUserInSecurityGroup instruments db.Store.IsUserInSecurityGroup.
func (s *InstrumentedStore) IsUserInSecurityGroup(ctx context.Context, userID db.UserID, groupID db.SecurityGroupID) (bool, error) {
	start := time.Now()
	ok, err := s.inner.IsUserInSecurityGroup(ctx, userID, groupID)
	s.observe("IsUserInSecurityGroup", start, err)
	return ok, err
}

// CountSecurityGroupMembers instruments db.Store.CountSecurityGroupMembers.
func (s *InstrumentedStore) CountSecurityGroupMembers(ctx context.Context, groupID db.SecurityGroupID) (int, error) {
	start := time.Now()
	n, err := s.inner.CountSecurityGroupMembers(ctx, groupID)
	s.observe("CountSecurityGroupMembers", start, err)
	return n, err
}

// --- Health ------------------------------------------------------------------

// Ping instruments db.Store.Ping.
func (s *InstrumentedStore) Ping(ctx context.Context) error {
	start := time.Now()
	err := s.inner.Ping(ctx)
	s.observe("Ping", start, err)
	return err
}

// Close instruments db.Store.Close.
func (s *InstrumentedStore) Close() error {
	return s.inner.Close()
}
