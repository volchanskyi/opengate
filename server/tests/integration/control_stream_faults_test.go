package integration

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func waitForDeviceStatus(t *testing.T, store *db.PostgresStore, deviceID protocol.DeviceID, want db.DeviceStatus) {
	t.Helper()
	devs := device.NewPostgresDevices(store.DB())
	require.Eventually(t, func() bool {
		dev, err := devs.Get(defaultTenantContext(), deviceID)
		return err == nil && dev.Status == want
	}, 3*time.Second, 25*time.Millisecond,
		"device %s never reached status %q", deviceID, want)
}

func setupOnlineAgent(t *testing.T) (*agentTestEnv, *quic.Stream, protocol.DeviceID) {
	t.Helper()
	env := newAgentTestEnv(t)
	ctx := context.Background()
	site := testutil.SeedSite(t, ctx, env.store)
	stream, deviceID := env.connectAgent(t, site.ID)
	waitForDeviceStatus(t, env.store, deviceID, db.StatusOnline)
	return env, stream, deviceID
}

func writeRawControlFrameHeader(t *testing.T, stream *quic.Stream, payloadLen uint32) {
	t.Helper()
	var header [5]byte
	header[0] = protocol.FrameControl
	binary.BigEndian.PutUint32(header[1:], payloadLen)
	_, err := stream.Write(header[:])
	require.NoError(t, err)
}

func writeCorruptedControlFrame(t *testing.T, stream *quic.Stream, garbage []byte) {
	t.Helper()
	writeRawControlFrameHeader(t, stream, uint32(len(garbage)))
	_, err := stream.Write(garbage)
	require.NoError(t, err)
}

func TestControlStream_CorruptedMsgpackPayloadDisconnectsAgent(t *testing.T) {
	t.Parallel()
	env, stream, deviceID := setupOnlineAgent(t)

	// 0xc1 is the reserved byte in MessagePack, so decoding always fails.
	writeCorruptedControlFrame(t, stream, []byte{0xc1, 0xc1, 0xc1, 0xc1})

	waitForDeviceStatus(t, env.store, deviceID, db.StatusOffline)
}

func TestControlStream_PartialFrameThenCloseDisconnectsAgent(t *testing.T) {
	t.Parallel()
	env, stream, deviceID := setupOnlineAgent(t)

	writeRawControlFrameHeader(t, stream, 256)
	_, err := stream.Write([]byte("partial-10"))
	require.NoError(t, err)
	require.NoError(t, stream.Close())

	waitForDeviceStatus(t, env.store, deviceID, db.StatusOffline)
}

func TestControlStream_ConcurrentServerInitiatedSendsArriveDecodable(t *testing.T) {
	t.Parallel()
	env, stream, deviceID := setupOnlineAgent(t)

	ac := env.srv.GetAgent(deviceID)
	require.NotNil(t, ac, "agent must be registered before issuing concurrent sends")

	errCh := make(chan error, 2)
	go func() { errCh <- ac.SendRequestHardwareReport(context.Background()) }()
	go func() { errCh <- ac.SendRequestDeviceLogs(context.Background(), device.LogFilter{}) }()
	for i := 0; i < 2; i++ {
		require.NoError(t, <-errCh, "concurrent send %d returned an error", i)
	}

	codec := &protocol.Codec{}
	seen := map[protocol.ControlMessageType]int{}
	for i := 0; i < 2; i++ {
		require.NoError(t, stream.SetReadDeadline(time.Now().Add(3*time.Second)))
		frameType, payload, err := codec.ReadFrame(stream)
		require.NoError(t, err, "frame %d ReadFrame failed (envelope corruption?)", i)
		require.Equal(t, protocol.FrameControl, frameType, "frame %d wrong type", i)
		msg, err := codec.DecodeControl(payload)
		require.NoError(t, err, "frame %d DecodeControl failed (payload corruption?)", i)
		seen[msg.Type]++
	}
	assert.Equal(t, 1, seen[protocol.MsgRequestHardwareReport], "RequestHardwareReport should appear exactly once")
	assert.Equal(t, 1, seen[protocol.MsgRequestDeviceLogs], "RequestDeviceLogs should appear exactly once")
}

func TestControlStream_SendAfterStreamCloseFailsAndReconciles(t *testing.T) {
	t.Parallel()
	env, stream, deviceID := setupOnlineAgent(t)

	ac := env.srv.GetAgent(deviceID)
	require.NotNil(t, ac, "agent must be registered before we close the stream")
	require.NoError(t, stream.Close())

	waitForDeviceStatus(t, env.store, deviceID, db.StatusOffline)

	// A QUIC write is accepted by the local send buffer whether or not the peer is still there,
	// so the refusal comes from the server itself.
	err := ac.SendRequestHardwareReport(context.Background())
	require.Error(t, err, "send on a released connection must surface an error")
	assert.True(t, errors.Is(err, agentapi.ErrConnectionClosed),
		"the error names the connection as gone, got %v", err)
}

func TestControlStream_ManyConcurrentSendsAllDecodable(t *testing.T) {
	t.Parallel()
	env, stream, deviceID := setupOnlineAgent(t)

	ac := env.srv.GetAgent(deviceID)
	require.NotNil(t, ac, "agent must be registered before issuing concurrent sends")

	// Two message types of different encoded sizes widen the interleaving window.
	const sends = 64
	errCh := make(chan error, sends)
	var wg sync.WaitGroup
	wg.Add(sends)
	for i := 0; i < sends; i++ {
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				errCh <- ac.SendRequestHardwareReport(context.Background())
			} else {
				errCh <- ac.SendRequestDeviceLogs(context.Background(), device.LogFilter{Search: "concurrent"})
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err, "a concurrent send returned an error")
	}

	codec := &protocol.Codec{}
	seen := map[protocol.ControlMessageType]int{}
	for i := 0; i < sends; i++ {
		require.NoError(t, stream.SetReadDeadline(time.Now().Add(5*time.Second)))
		frameType, payload, err := codec.ReadFrame(stream)
		require.NoError(t, err, "frame %d ReadFrame failed (envelope corruption?)", i)
		require.Equal(t, protocol.FrameControl, frameType, "frame %d wrong type", i)
		msg, err := codec.DecodeControl(payload)
		require.NoError(t, err, "frame %d DecodeControl failed (payload corruption?)", i)
		seen[msg.Type]++
	}
	assert.Equal(t, sends/2, seen[protocol.MsgRequestHardwareReport])
	assert.Equal(t, sends/2, seen[protocol.MsgRequestDeviceLogs])
}
