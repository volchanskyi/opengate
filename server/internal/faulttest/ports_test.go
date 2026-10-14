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

type fakeSessions struct {
	session.Repository
	createCalls int
	getCalls    int
}

func (f *fakeSessions) Create(context.Context, *session.Session) (string, error) {
	f.createCalls++
	return "", nil
}

func (f *fakeSessions) Get(context.Context, string) (*session.Session, error) {
	f.getCalls++
	return &session.Session{}, nil
}

func TestFaultRegistryPingFaults(t *testing.T) {
	t.Parallel()
	fr := WrapRegistry(relay.NewInProcessRegistry())

	require.NoError(t, fr.Ping(context.Background()))

	fr.Arm("Ping", Spec{Action: ActionError})
	assert.ErrorIs(t, fr.Ping(context.Background()), ErrInjected)
}

type fakeAgentControl struct {
	api.AgentControl
	sendCalls int
}

func (f *fakeAgentControl) SendSessionRequest(context.Context, protocol.SessionToken, string, protocol.Permissions, []string) error {
	f.sendCalls++
	return nil
}

func TestFaultAgentControlBlockedExitsOnCancel(t *testing.T) {
	t.Parallel()
	fac := WrapAgentControl(&fakeAgentControl{})
	fac.Arm("SendSessionRequest", Spec{Action: ActionBlocked})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- fac.SendSessionRequest(ctx, "tok", "wss://relay", protocol.Permissions{}, nil)
	}()
	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("blocked control-write did not exit after context cancellation")
	}
}

// armable is the fault surface every port decorator shares.
type armable interface {
	Arm(method string, s Spec)
	Clear(method string)
}

func TestEveryPortFaultSkipsTheRealCallUntilCleared(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	devices, sessions, agents := &recordingDevices{}, &fakeSessions{}, &fakeAgentControl{}
	fd, fs, fac := WrapDevices(devices), WrapSessions(sessions), WrapAgentControl(agents)
	fr := WrapRegistry(relay.NewInProcessRegistry())

	for _, c := range []struct {
		name   string
		port   armable
		method string
		call   func() error
		// realCalls is nil where the real port keeps no count.
		realCalls func() int
	}{
		{"device list", fd, "List", func() error {
			_, err := fd.List(ctx, device.Filter{SiteID: uuid.New()})
			return err
		}, func() int { return devices.listCalls }},
		{"session create", fs, "Create", func() error {
			_, err := fs.Create(ctx, &session.Session{})
			return err
		}, func() int { return sessions.createCalls }},
		{"session get", fs, "Get", func() error {
			_, err := fs.Get(ctx, "tok")
			return err
		}, func() int { return sessions.getCalls }},
		{"session request to an agent", fac, "SendSessionRequest", func() error {
			return fac.SendSessionRequest(ctx, "tok", "wss://relay", protocol.Permissions{}, nil)
		}, func() int { return agents.sendCalls }},
		{"relay session save", fr, "SaveSession", func() error {
			return fr.SaveSession(ctx, "tok", relay.SessionMeta{ServerID: "test-server"})
		}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.port.Arm(c.method, Spec{Action: ActionError})
			assert.ErrorIs(t, c.call(), ErrInjected)
			if c.realCalls != nil {
				assert.Equal(t, 0, c.realCalls(), "a faulted call never reaches the real port")
			}

			c.port.Clear(c.method)
			require.NoError(t, c.call())
			if c.realCalls != nil {
				assert.Equal(t, 1, c.realCalls())
			}
		})
	}
}
