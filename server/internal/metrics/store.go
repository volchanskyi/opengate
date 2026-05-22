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
	s.metrics.Observe(operation, time.Since(start), err == nil)
}

// Observe records a single DB-shaped operation against the standard db_query_*
// metric pair. It lets audit.Instrumented (and other repository decorators
// added under ADR-021) reuse the same dashboards without importing this
// package or duplicating label discipline.
func (m *Metrics) Observe(operation string, duration time.Duration, ok bool) {
	status := "ok"
	if !ok {
		status = "error"
	}
	m.DBQueryDuration.WithLabelValues(operation).Observe(duration.Seconds())
	m.DBQueriesTotal.WithLabelValues(operation, status).Inc()
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
