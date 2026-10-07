package faulttest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/api"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/session"
)

type recordingDevices struct {
	device.Repository
	gotTenant dbtx.Tenant
	getCalls  int
	listCalls int
}

func (r *recordingDevices) Get(ctx context.Context, _ device.DeviceID) (*device.Device, error) {
	r.getCalls++
	if tenant, ok := dbtx.TenantFromContext(ctx); ok {
		r.gotTenant = tenant
	}
	return &device.Device{}, nil
}

func (r *recordingDevices) List(context.Context, device.Filter) ([]*device.Device, error) {
	r.listCalls++
	return nil, nil
}

func TestFaultDevicesDelegatesWhenUnarmed(t *testing.T) {
	t.Parallel()
	real := &recordingDevices{}
	fd := WrapDevices(real)

	_, err := fd.Get(context.Background(), device.DeviceID(uuid.New()))
	require.NoError(t, err)
	assert.Equal(t, 1, real.getCalls, "unarmed decorator must delegate to the real repository")
}

func TestFaultDevicesErrorSkipsRealCall(t *testing.T) {
	t.Parallel()
	real := &recordingDevices{}
	fd := WrapDevices(real)
	fd.Arm("Get", Spec{Action: ActionError})

	_, err := fd.Get(context.Background(), device.DeviceID(uuid.New()))
	assert.ErrorIs(t, err, ErrInjected)
	assert.Equal(t, 0, real.getCalls, "an ActionError fault must not reach the real repository")
}

func TestFaultDevicesPreservesTenantContext(t *testing.T) {
	t.Parallel()
	real := &recordingDevices{}
	fd := WrapDevices(real)

	tenantA := uuid.New()
	ctxA := dbtx.WithTenant(context.Background(), tenantA, false)
	_, err := fd.Get(ctxA, device.DeviceID(uuid.New()))
	require.NoError(t, err)
	assert.Equal(t, tenantA, real.gotTenant.TenantID, "tenant A context must reach the repository unchanged")

	tenantB := uuid.New()
	ctxB := dbtx.WithTenant(context.Background(), tenantB, false)
	fd.Arm("Get", Spec{Action: ActionDelay, Delay: 0})
	_, err = fd.Get(ctxB, device.DeviceID(uuid.New()))
	require.NoError(t, err)
	assert.Equal(t, tenantB, real.gotTenant.TenantID, "a fault must not cross tenant context — tenant B must not observe tenant A")
}

func TestFaultDevicesListFaultsAndDelegates(t *testing.T) {
	t.Parallel()
	real := &recordingDevices{}
	fd := WrapDevices(real)

	fd.Arm("List", Spec{Action: ActionError})
	_, err := fd.List(context.Background(), device.Filter{SiteID: uuid.New()})
	assert.ErrorIs(t, err, ErrInjected)
	assert.Equal(t, 0, real.listCalls)

	fd.Clear("List")
	_, err = fd.List(context.Background(), device.Filter{SiteID: uuid.New()})
	require.NoError(t, err)
	assert.Equal(t, 1, real.listCalls)
}

type fakeSessions struct {
	session.Repository
	createCalls int
	getCalls    int
}

func (f *fakeSessions) Create(context.Context, *session.Session) error {
	f.createCalls++
	return nil
}

func (f *fakeSessions) Get(context.Context, string) (*session.Session, error) {
	f.getCalls++
	return &session.Session{}, nil
}

func TestFaultSessionsErrorSkipsRealCall(t *testing.T) {
	t.Parallel()
	real := &fakeSessions{}
	fs := WrapSessions(real)
	fs.Arm("Create", Spec{Action: ActionError})

	err := fs.Create(context.Background(), &session.Session{})
	assert.ErrorIs(t, err, ErrInjected)
	assert.Equal(t, 0, real.createCalls)

	fs.Clear("Create")
	require.NoError(t, fs.Create(context.Background(), &session.Session{}))
	assert.Equal(t, 1, real.createCalls)
}

func TestFaultSessionsGetFaultsAndDelegates(t *testing.T) {
	t.Parallel()
	real := &fakeSessions{}
	fs := WrapSessions(real)

	fs.Arm("Get", Spec{Action: ActionError})
	_, err := fs.Get(context.Background(), "tok")
	assert.ErrorIs(t, err, ErrInjected)
	assert.Equal(t, 0, real.getCalls)

	fs.Clear("Get")
	_, err = fs.Get(context.Background(), "tok")
	require.NoError(t, err)
	assert.Equal(t, 1, real.getCalls)
}

func TestFaultRegistryPingFaults(t *testing.T) {
	t.Parallel()
	fr := WrapRegistry(relay.NewInProcessRegistry())

	require.NoError(t, fr.Ping(context.Background()))

	fr.Arm("Ping", Spec{Action: ActionError})
	assert.ErrorIs(t, fr.Ping(context.Background()), ErrInjected)
}

func TestFaultRegistrySaveSessionFaultsAndDelegates(t *testing.T) {
	t.Parallel()
	fr := WrapRegistry(relay.NewInProcessRegistry())
	meta := relay.SessionMeta{ServerID: "test-server"}

	fr.Arm("SaveSession", Spec{Action: ActionError})
	assert.ErrorIs(t, fr.SaveSession(context.Background(), "tok", meta), ErrInjected)

	fr.Clear("SaveSession")
	require.NoError(t, fr.SaveSession(context.Background(), "tok", meta))
}

type fakeAgentControl struct {
	api.AgentControl
	sendCalls int
}

func (f *fakeAgentControl) SendSessionRequest(context.Context, protocol.SessionToken, string, protocol.Permissions) error {
	f.sendCalls++
	return nil
}

func TestFaultAgentControlSendFaults(t *testing.T) {
	t.Parallel()
	real := &fakeAgentControl{}
	fac := WrapAgentControl(real)
	fac.Arm("SendSessionRequest", Spec{Action: ActionError})

	err := fac.SendSessionRequest(context.Background(), "tok", "wss://relay", protocol.Permissions{})
	assert.ErrorIs(t, err, ErrInjected)
	assert.Equal(t, 0, real.sendCalls, "a faulted control-write must not reach the real agent")

	fac.Clear("SendSessionRequest")
	require.NoError(t, fac.SendSessionRequest(context.Background(), "tok", "wss://relay", protocol.Permissions{}))
	assert.Equal(t, 1, real.sendCalls)
}

func TestFaultAgentControlBlockedExitsOnCancel(t *testing.T) {
	t.Parallel()
	fac := WrapAgentControl(&fakeAgentControl{})
	fac.Arm("SendSessionRequest", Spec{Action: ActionBlocked})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- fac.SendSessionRequest(ctx, "tok", "wss://relay", protocol.Permissions{})
	}()
	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("blocked control-write did not exit after context cancellation")
	}
}
