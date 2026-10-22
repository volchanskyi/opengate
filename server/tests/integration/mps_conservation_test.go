package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/amt/transport"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// deviceMode is how the simulated device treats the channels opened while it is set.
type deviceMode int32

const (
	// answered channels are confirmed and every request on them gets a reply.
	answered deviceMode = iota
	// stalled channels are confirmed and their requests are read and never answered.
	stalled
	// unconfirmed channel opens are never answered.
	unconfirmed
)

// mpsOperationTimeout bounds each request; stalled and unconfirmed ones end at it.
const mpsOperationTimeout = 200 * time.Millisecond

// mpsGoroutineSlopeTolerance is the goroutines per finished request the fit may carry; a client
// that ignores its deadline read 0.34 per request, and the fixed path reads 0.
const mpsGoroutineSlopeTolerance = 0.2

// mpsHeapSlopeTolerance is the retained bytes per finished request the fit may carry; a client
// that ignores its deadline read 2.6 KiB per request, and the fixed path reads 33 to 270 B.
const mpsHeapSlopeTolerance = 1 << 10

func TestMPSRequestsConserveGoroutinesAndHeap(t *testing.T) {
	store := testutil.NewTestStore(t)
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mps := transport.NewServer(cm, testutil.NewTestAMTDevices(t, store), testutil.NewTestHardware(t, store), logger)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = mps.ListenAndServe(ctx, "127.0.0.1:0") }()
	service := amt.NewService(mps, "admin", "P@ssw0rd", logger)

	amtUUID := uuid.New()
	device := startSimulatedDevice(t, mps.Addr(), cm.CACertPEM(), amtUUID)
	require.Eventually(t, func() bool { return mps.GetConn(amtUUID) != nil },
		5*time.Second, 10*time.Millisecond, "the device must be connected before requests are sent")

	// The first rounds warm the pools and the TLS session, so they run before measurement.
	for i := range 3 {
		device.runRequest(t, service, amtUUID, deviceMode(i))
	}
	settleProcess(t)

	var xs, goroutines, heap []float64
	finished := 0
	for _, point := range conservationPoints {
		for ; finished < point; finished++ {
			device.runRequest(t, service, amtUUID, deviceMode(finished%3))
		}
		liveGoroutines, liveHeap := settleProcess(t)
		xs = append(xs, float64(point))
		goroutines = append(goroutines, float64(liveGoroutines))
		heap = append(heap, float64(liveHeap))
	}

	goroutineSlope := slopeThrough(xs, goroutines)
	heapSlope := slopeThrough(xs, heap)
	t.Logf("finished requests %v → goroutines %v (%.3f/request), heap %v (%.0f B/request)",
		xs, goroutines, goroutineSlope, heap, heapSlope)
	assert.Equal(t, int64(finished+3), device.answers.Load()+device.stalls.Load()+device.ignored.Load(),
		"every request reached the device")

	assert.LessOrEqualf(t, math.Abs(goroutineSlope), mpsGoroutineSlopeTolerance,
		"a finished Intel AMT request must give back its goroutines: %.3f retained per request", goroutineSlope)
	assert.LessOrEqualf(t, math.Abs(heapSlope), float64(mpsHeapSlopeTolerance),
		"a finished Intel AMT request must give back its heap: %.0f bytes retained per request", heapSlope)
}

// simulatedDevice answers the server over one CIRA connection the way its mode says.
type simulatedDevice struct {
	conn    net.Conn
	mode    atomic.Int32
	answers atomic.Int64
	stalls  atomic.Int64
	ignored atomic.Int64

	mu       sync.Mutex
	channels map[uint32]*deviceChannel
	nextID   uint32
}

type deviceChannel struct {
	serverID uint32
	mode     deviceMode
	request  bytes.Buffer
}

// runRequest sends one device-information request with the device in `mode`, and waits for it
// to return; a request outliving its deadline by a second is left running for the fit to count.
func (d *simulatedDevice) runRequest(t *testing.T, service *amt.Service, amtUUID uuid.UUID, mode deviceMode) {
	t.Helper()
	d.mode.Store(int32(mode))
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), mpsOperationTimeout)
		defer cancel()
		_, err := service.QueryDeviceInfo(ctx, amtUUID)
		done <- err
	}()
	select {
	case err := <-done:
		if mode == answered {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, context.DeadlineExceeded)
		}
	case <-time.After(mpsOperationTimeout + time.Second):
	}
}

func startSimulatedDevice(t *testing.T, addr string, caPEM []byte, amtUUID uuid.UUID) *simulatedDevice {
	t.Helper()
	conn := dialSimulatedCIRA(t, addr, caPEM, amtUUID)
	d := &simulatedDevice{conn: conn, channels: map[uint32]*deviceChannel{}, nextID: 1000}
	go d.serve()
	t.Cleanup(func() { _ = conn.Close() })
	return d
}

func (d *simulatedDevice) serve() {
	for {
		msgType, payload, err := transport.ReadMessage(d.conn)
		if err != nil {
			return
		}
		if err := d.handle(msgType, payload); err != nil {
			return
		}
	}
}

func (d *simulatedDevice) handle(msgType uint8, payload []byte) error {
	switch msgType {
	case transport.APFChannelOpen:
		return d.open(payload)
	case transport.APFChannelData:
		return d.data(payload)
	case transport.APFChannelClose:
		return d.close(payload)
	case transport.APFKeepaliveRequest:
		return transport.WriteKeepaliveReply(d.conn, binary.BigEndian.Uint32(payload))
	default:
		return nil
	}
}

func (d *simulatedDevice) open(payload []byte) error {
	co, err := transport.ParseChannelOpen(payload)
	if err != nil {
		return err
	}
	mode := deviceMode(d.mode.Load())
	if mode == unconfirmed {
		d.ignored.Add(1)
		return nil
	}
	d.mu.Lock()
	id := d.nextID
	d.nextID++
	d.channels[id] = &deviceChannel{serverID: co.SenderChannel, mode: mode}
	d.mu.Unlock()
	return transport.WriteChannelOpenConfirm(d.conn, co.SenderChannel, id,
		transport.DefaultWindowSize, transport.DefaultMaxPacketSize)
}

func (d *simulatedDevice) data(payload []byte) error {
	cd, err := transport.ParseChannelData(payload)
	if err != nil {
		return err
	}
	d.mu.Lock()
	ch, ok := d.channels[cd.RecipientChannel]
	d.mu.Unlock()
	if !ok {
		return nil
	}
	ch.request.Write(cd.Data)
	if !completeRequest(ch.request.Bytes()) {
		return nil
	}
	ch.request.Reset()
	if ch.mode == stalled {
		d.stalls.Add(1)
		return nil
	}
	d.answers.Add(1)
	return transport.WriteChannelData(d.conn, ch.serverID, deviceInfoResponse())
}

func (d *simulatedDevice) close(payload []byte) error {
	id := binary.BigEndian.Uint32(payload)
	d.mu.Lock()
	ch, ok := d.channels[id]
	delete(d.channels, id)
	d.mu.Unlock()
	if !ok {
		return nil
	}
	return transport.WriteChannelClose(d.conn, ch.serverID)
}

// completeRequest reports whether buf holds an HTTP request's headers and its whole body.
func completeRequest(buf []byte) bool {
	headerEnd := bytes.Index(buf, []byte("\r\n\r\n"))
	if headerEnd < 0 {
		return false
	}
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(buf)))
	if err != nil {
		return false
	}
	return len(buf)-(headerEnd+4) >= max(0, int(req.ContentLength))
}

func deviceInfoResponse() []byte {
	body := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>` +
		`<p:CIM_ComputerSystem><p:Name>amt-host</p:Name><p:Model>Optiplex</p:Model>` +
		`</p:CIM_ComputerSystem></s:Body></s:Envelope>`
	return fmt.Appendf(nil, "HTTP/1.1 200 OK\r\nContent-Type: application/soap+xml\r\n"+
		"Content-Length: %d\r\n\r\n%s", len(body), body)
}

// dialSimulatedCIRA connects to the MPS and completes the handshake an Intel AMT device makes.
func dialSimulatedCIRA(t *testing.T, addr string, caPEM []byte, amtUUID uuid.UUID) net.Conn {
	t.Helper()
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(caPEM), "the test CA should be parseable")
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr,
		&tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12})
	require.NoError(t, err)

	version := make([]byte, 29)
	version[0] = transport.APFProtocolVersion
	version[4] = 1
	copy(version[13:], []byte{
		amtUUID[3], amtUUID[2], amtUUID[1], amtUUID[0], amtUUID[5], amtUUID[4], amtUUID[7], amtUUID[6],
	})
	copy(version[21:], amtUUID[8:])
	sendCIRA(t, conn, version, transport.APFProtocolVersion)
	sendCIRA(t, conn, ciraString(transport.APFServiceRequest, transport.ServiceAuth), transport.APFServiceAccept)

	auth := []byte{transport.APFUserAuthRequest}
	for _, field := range []string{"admin", transport.ServiceAuth, "digest"} {
		auth = append(auth, ciraString(0, field)[1:]...)
	}
	sendCIRA(t, conn, auth, transport.APFUserAuthSuccess)
	sendCIRA(t, conn, ciraString(transport.APFServiceRequest, transport.ServicePFwd), transport.APFServiceAccept)

	forward := ciraString(transport.APFGlobalRequest, "tcpip-forward")
	forward = append(forward, 1)
	forward = append(forward, ciraString(0, "")[1:]...)
	forward = binary.BigEndian.AppendUint32(forward, 16992)
	sendCIRA(t, conn, forward, transport.APFRequestSuccess)
	return conn
}

func sendCIRA(t *testing.T, conn net.Conn, msg []byte, reply uint8) {
	t.Helper()
	_, err := conn.Write(msg)
	require.NoError(t, err)
	got, _, err := transport.ReadMessage(conn)
	require.NoError(t, err)
	require.Equal(t, reply, got)
}

// ciraString is a message of type msgType carrying one length-prefixed string.
func ciraString(msgType uint8, s string) []byte {
	msg := binary.BigEndian.AppendUint32([]byte{msgType}, uint32(len(s)))
	return append(msg, s...)
}
