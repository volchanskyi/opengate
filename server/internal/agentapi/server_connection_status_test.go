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

// recordingDevices stands in for the device repository so a test can say when
// a status write reaches the database and in what order the writes landed. It
// embeds the interface and implements only SetStatus: nothing else on the
// repository is reachable from the connection-teardown path under test.
type recordingDevices struct {
	device.Repository

	// beforeWrite runs on the calling goroutine before a write is recorded, so
	// a test can hold one write open while it drives the other side.
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

// TestAMachineDiallingBackWaitsForTheDepartingOfflineWrite states the ordering
// a technician's device list depends on. A machine that drops and dials
// straight back has two connections in flight at once, and both write its
// status: the departing one writes offline, the returning one writes online.
// Deciding ownership from the connection map and only then reaching the
// database leaves those two writes unordered — the offline write can land last
// and a connected machine reads offline until something else moves it. The
// decision and the write it authorises are one step, so a returning connection
// is not registered while the departing one is still writing.
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

// TestASupersededConnectionWritesNothing is the other half: a teardown that
// finds the machine already dialled back leaves the row alone rather than
// writing over a status that is already true.
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

// TestTheDeviceStatusGateIsDroppedWhenNobodyHoldsIt keeps the per-device gate
// from becoming a map of every device the server has ever seen: a gate exists
// only while a transition is in flight.
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
