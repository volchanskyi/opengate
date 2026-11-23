package agentapi

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// recordingDevices implements only SignIn and SetStatus, recording each status it writes; the
// embedded interface is nil for every other method.
type recordingDevices struct {
	device.Repository

	beforeWrite func(device.DeviceStatus)

	mu      sync.Mutex
	written []device.DeviceStatus
}

func (r *recordingDevices) SetStatus(_ context.Context, _ device.DeviceID, status device.DeviceStatus) error {
	if r.beforeWrite != nil {
		r.beforeWrite(status)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.written = append(r.written, status)
	return nil
}

func (r *recordingDevices) SignIn(context.Context, *device.Device, func(device.OrganizationID) bool) (device.SignIn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.written = append(r.written, device.StatusOnline)
	return device.SignIn{}, nil
}

func (r *recordingDevices) statuses() []device.DeviceStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]device.DeviceStatus(nil), r.written...)
}

func TestAMachineDiallingBackWaitsForTheDepartingOfflineWrite(t *testing.T) {
	srv := newTestAgentServer(t)
	deviceID := protocol.DeviceID(uuid.New())

	writing := make(chan struct{})
	release := make(chan struct{})
	devices := &recordingDevices{}
	devices.beforeWrite = func(status device.DeviceStatus) {
		if status != device.StatusOffline {
			return
		}
		close(writing)
		<-release
	}
	srv.devices = devices

	departing := &AgentConn{DeviceID: deviceID}
	holdSignedIn(context.Background(), srv, departing, "contoso-workstation")

	released := make(chan struct{})
	go func() {
		defer close(released)
		srv.releaseDeviceStatus(departing, "contoso-workstation", testLogger())
	}()
	<-writing

	returning := &AgentConn{DeviceID: deviceID, stream: registerFrame(t), codec: &protocol.Codec{},
		devices: devices, logger: testLogger()}
	registered := make(chan struct{})
	go func() {
		defer close(registered)
		_, _, leaveLine, err := srv.signIn(context.Background(), returning)
		if assert.NoError(t, err) {
			leaveLine()
		}
	}()

	assert.Never(t, func() bool {
		select {
		case <-registered:
			return true
		default:
			return false
		}
	}, 250*time.Millisecond, 10*time.Millisecond,
		"a machine dialling back must not be registered while the connection it replaces is still writing it offline")

	close(release)
	<-released
	<-registered

	assert.Equal(t, []device.DeviceStatus{device.StatusOffline, device.StatusOnline}, devices.statuses(),
		"the departing connection writes offline once, and the sign-in writes online after it")
	assert.Equal(t, 1, srv.ConnectedAgentCount(),
		"one machine is one machine, however many connections it has open")
}

func TestASupersededConnectionWritesNothing(t *testing.T) {
	srv := newTestAgentServer(t)
	deviceID := protocol.DeviceID(uuid.New())

	devices := &recordingDevices{}
	srv.devices = devices

	departing := &AgentConn{DeviceID: deviceID}
	holdSignedIn(context.Background(), srv, departing, "contoso-workstation")
	holdSignedIn(context.Background(), srv, &AgentConn{DeviceID: deviceID}, "contoso-workstation")

	srv.releaseDeviceStatus(departing, "contoso-workstation", testLogger())

	assert.Empty(t, devices.statuses(),
		"a superseded connection does not take a live machine offline")
	assert.Equal(t, 1, srv.ConnectedAgentCount())
	require.NotNil(t, srv.GetAgent(deviceID), "the returning connection survives")
}

func TestTheDeviceStatusGateIsDroppedWhenNobodyHoldsIt(t *testing.T) {
	srv := newTestAgentServer(t)
	deviceID := protocol.DeviceID(uuid.New())
	devices := &recordingDevices{}
	srv.devices = devices

	conn := &AgentConn{DeviceID: deviceID}
	holdSignedIn(context.Background(), srv, conn, "contoso-workstation")
	srv.releaseDeviceStatus(conn, "contoso-workstation", testLogger())

	assert.Empty(t, srv.statusGate.inFlight(),
		"a device nobody is transitioning holds no gate")
}

func TestALateTeardownOfAReplacedConnectionKeepsTheReturningGrant(t *testing.T) {
	env := newAcceptEnv(t)
	deviceID := protocol.DeviceID(uuid.New())

	departing := &AgentConn{DeviceID: deviceID}
	holdSignedIn(context.Background(), env.srv, departing, "contoso-workstation")
	holdSignedIn(context.Background(), env.srv, &AgentConn{DeviceID: deviceID}, "contoso-workstation")
	env.srv.scheduler.RequestSlot(deviceID, uuid.New())

	conn, stream := env.dial(t, uuid.New())
	env.srv.unregisterConn(stream, conn, departing, "contoso-workstation", testLogger())

	admission := env.srv.scheduler.Admit(CatchUpBatch{Device: deviceID, Key: "minute-1", Size: 1})
	assert.Equal(t, Admitted, admission.Verdict,
		"the returning connection keeps the catch-up grant it took (refused: %q)", admission.Reason)
}

func TestTheCurrentConnectionLeavingDropsItsGrant(t *testing.T) {
	env := newAcceptEnv(t)
	deviceID := protocol.DeviceID(uuid.New())

	current := &AgentConn{DeviceID: deviceID}
	holdSignedIn(context.Background(), env.srv, current, "contoso-workstation")
	env.srv.scheduler.RequestSlot(deviceID, uuid.New())

	conn, stream := env.dial(t, uuid.New())
	env.srv.unregisterConn(stream, conn, current, "contoso-workstation", testLogger())

	admission := env.srv.scheduler.Admit(CatchUpBatch{Device: deviceID, Key: "minute-1", Size: 1})
	assert.Equal(t, BackfillNoGrant, admission.Reason, "a machine that left holds no catch-up grant")
}

func TestALateTeardownOfAReplacedConnectionKeepsTheReturningRuleCoverage(t *testing.T) {
	env := newAcceptEnv(t)
	deviceID := protocol.DeviceID(uuid.New())

	departing := &AgentConn{DeviceID: deviceID}
	holdSignedIn(context.Background(), env.srv, departing, "contoso-workstation")
	holdSignedIn(context.Background(), env.srv, &AgentConn{DeviceID: deviceID}, "contoso-workstation")
	env.srv.coverage.Report(deviceID, []protocol.RuleCoverage{{RuleID: "disk-critical", State: protocol.RuleCoverageActive}})

	conn, stream := env.dial(t, uuid.New())
	env.srv.unregisterConn(stream, conn, departing, "contoso-workstation", testLogger())

	assert.Equal(t, 1, env.srv.coverage.Aggregate(1, nil)["disk-critical"].Active,
		"the returning connection's rule coverage outlives the teardown of the one it replaced")
}

func TestTheCurrentConnectionLeavingForgetsItsRuleCoverage(t *testing.T) {
	env := newAcceptEnv(t)
	deviceID := protocol.DeviceID(uuid.New())

	current := &AgentConn{DeviceID: deviceID}
	holdSignedIn(context.Background(), env.srv, current, "contoso-workstation")
	env.srv.coverage.Report(deviceID, []protocol.RuleCoverage{{RuleID: "disk-critical", State: protocol.RuleCoverageActive}})

	conn, stream := env.dial(t, uuid.New())
	env.srv.unregisterConn(stream, conn, current, "contoso-workstation", testLogger())

	assert.Zero(t, env.srv.coverage.Aggregate(1, nil)["disk-critical"].Active,
		"a machine that left is unknown to the rule coverage")
}

// holdSignedIn holds ac in the server's map and announces it online, as a written sign-in does.
func holdSignedIn(ctx context.Context, srv *AgentServer, ac *AgentConn, hostname string) {
	srv.swapIn(ctx, ac)
	srv.announceOnline(ctx, ac, hostname)
}

// registerFrame is a control stream holding one AgentRegister, as a machine opens with.
func registerFrame(t *testing.T) *bytes.Buffer {
	t.Helper()
	codec := &protocol.Codec{}
	payload, err := codec.EncodeControl(&protocol.ControlMessage{
		Type: protocol.MsgAgentRegister, Hostname: "contoso-workstation", OS: "linux", Arch: "amd64",
	})
	require.NoError(t, err)
	buf := &bytes.Buffer{}
	require.NoError(t, codec.WriteFrame(buf, protocol.FrameControl, payload))
	return buf
}
