package agentapi

import (
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

// recordingDevices implements only SetStatus; the embedded interface is nil for every other method.
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
	srv.registerConn(context.Background(), departing, "contoso-workstation")

	released := make(chan struct{})
	go func() {
		defer close(released)
		srv.releaseDeviceStatus(departing, "contoso-workstation", testLogger())
	}()
	<-writing

	registered := make(chan struct{})
	go func() {
		defer close(registered)
		srv.registerConn(context.Background(), &AgentConn{DeviceID: deviceID}, "contoso-workstation")
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

	assert.Equal(t, []device.DeviceStatus{device.StatusOffline}, devices.statuses(),
		"the departing connection writes offline exactly once")
	assert.Equal(t, 1, srv.ConnectedAgentCount(),
		"one machine is one machine, however many connections it has open")
}

func TestASupersededConnectionWritesNothing(t *testing.T) {
	srv := newTestAgentServer(t)
	deviceID := protocol.DeviceID(uuid.New())

	devices := &recordingDevices{}
	srv.devices = devices

	departing := &AgentConn{DeviceID: deviceID}
	srv.registerConn(context.Background(), departing, "contoso-workstation")
	srv.registerConn(context.Background(), &AgentConn{DeviceID: deviceID}, "contoso-workstation")

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
	srv.registerConn(context.Background(), conn, "contoso-workstation")
	srv.releaseDeviceStatus(conn, "contoso-workstation", testLogger())

	assert.Empty(t, srv.statusGate.inFlight(),
		"a device nobody is transitioning holds no gate")
}
