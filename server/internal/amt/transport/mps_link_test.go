package transport_test

// The external test package can import amt, which package transport cannot, so the real
// repositories run here.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/amt/transport"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

type linkEnv struct {
	srv      *transport.Server
	addr     string
	caPEM    []byte
	store    *db.PostgresStore
	hardware device.HardwareRepository
	amtRepo  amt.Repository
}

func newLinkEnv(t *testing.T) *linkEnv {
	t.Helper()
	store := testutil.NewTestStore(t)
	hardware := testutil.NewTestHardware(t, store)
	amtRepo := testutil.NewTestAMTDevices(t, store)

	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	srv := transport.NewServer(cm, amtRepo, hardware, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.ListenAndServe(ctx, "127.0.0.1:0") }()

	return &linkEnv{
		srv:      srv,
		addr:     srv.Addr(),
		caPEM:    cm.CACertPEM(),
		store:    store,
		hardware: hardware,
		amtRepo:  amtRepo,
	}
}

func seedDeviceWithSystemUUID(t *testing.T, tenantCtx context.Context, env *linkEnv, systemUUID uuid.UUID) *device.Device {
	t.Helper()
	site := testutil.SeedSite(t, tenantCtx, env.store)
	dev := testutil.SeedDevice(t, tenantCtx, env.store, site.ID)
	available := true
	require.NoError(t, env.hardware.Upsert(tenantCtx, &device.Hardware{
		DeviceID:     dev.ID,
		CPUModel:     "Intel Core i7-12700K",
		SystemUUID:   &systemUUID,
		AMTAvailable: &available,
		AMTVersion:   "16.1.30.2260",
	}))
	return dev
}

func amtRow(t *testing.T, tenantCtx context.Context, env *linkEnv, amtUUID uuid.UUID) *db.AMTDevice {
	t.Helper()
	tenant, ok := dbtx.TenantFromContext(tenantCtx)
	require.True(t, ok, "the reader needs a tenant to scope by")

	var row db.AMTDevice
	err := env.store.DB().QueryRowContext(tenantCtx,
		`SELECT uuid, device_id, status FROM amt_devices WHERE tenant_id = $1 AND uuid = $2`,
		tenant.TenantID, amtUUID).Scan(&row.UUID, &row.DeviceID, &row.Status)
	if err != nil {
		return nil
	}
	return &row
}

func TestCIRAConnectPersistsUnderTheDeviceTenant(t *testing.T) {
	env := newLinkEnv(t)
	tenantB := uuid.New()
	testutil.EnsureTenant(t, context.Background(), env.store, tenantB, "Tenant "+tenantB.String()[:8])
	tenantCtx := dbtx.WithTenant(context.Background(), tenantB, false)

	amtUUID := uuid.New()
	dev := seedDeviceWithSystemUUID(t, tenantCtx, env, amtUUID)

	conn := dialCIRA(t, env, amtUUID)
	t.Cleanup(func() { _ = conn.Close() })

	require.Eventually(t, func() bool { return env.srv.GetConn(amtUUID) != nil },
		5*time.Second, 10*time.Millisecond, "server should register the CIRA connection")
	require.Eventually(t, func() bool {
		row := amtRow(t, tenantCtx, env, amtUUID)
		return row != nil && row.Status == db.StatusOnline
	}, 5*time.Second, 10*time.Millisecond, "the AMT row should be persisted online in the device's tenant")

	row := amtRow(t, tenantCtx, env, amtUUID)
	require.NotNil(t, row)
	assert.Equal(t, dev.ID, row.DeviceID, "the row should point at the device that reported this system UUID")

	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool {
		row := amtRow(t, tenantCtx, env, amtUUID)
		return row != nil && row.Status == db.StatusOffline
	}, 5*time.Second, 10*time.Millisecond, "disconnect should mark the row offline in the device's tenant")
}

func TestCIRAConnectWithNoDevicePersistsNothing(t *testing.T) {
	env := newLinkEnv(t)
	tenantCtx := dbtx.WithDefaultTenant(context.Background(), true)
	amtUUID := uuid.New()

	conn := dialCIRA(t, env, amtUUID)
	t.Cleanup(func() { _ = conn.Close() })

	require.Eventually(t, func() bool { return env.srv.GetConn(amtUUID) != nil },
		5*time.Second, 10*time.Millisecond, "the unmatched connection should still be held in memory")

	assert.Never(t, func() bool { return amtRow(t, tenantCtx, env, amtUUID) != nil },
		time.Second, 50*time.Millisecond, "an unmatched CIRA connection must persist nothing")
	assert.Equal(t, 1, env.srv.ConnectedDeviceCount(), "the connection stays live for a later keepalive to adopt")
}

func dialCIRA(t *testing.T, env *linkEnv, amtUUID uuid.UUID) net.Conn {
	t.Helper()
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(env.caPEM), "the test CA should be parseable")

	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 5 * time.Second},
		"tcp", env.addr,
		&tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12},
	)
	require.NoError(t, err)

	pv := make([]byte, 29)
	pv[0] = transport.APFProtocolVersion
	pv[4] = 1
	copy(pv[13:], intelGUID(amtUUID))
	writeAll(t, conn, pv)
	expect(t, conn, transport.APFProtocolVersion)

	writeAll(t, conn, apfStringMsg(transport.APFServiceRequest, transport.ServiceAuth))
	expect(t, conn, transport.APFServiceAccept)

	auth := []byte{transport.APFUserAuthRequest}
	auth = append(auth, apfString("admin")...)
	auth = append(auth, apfString(transport.ServiceAuth)...)
	auth = append(auth, apfString("digest")...)
	writeAll(t, conn, auth)
	expect(t, conn, transport.APFUserAuthSuccess)

	writeAll(t, conn, apfStringMsg(transport.APFServiceRequest, transport.ServicePFwd))
	expect(t, conn, transport.APFServiceAccept)

	fwd := []byte{transport.APFGlobalRequest}
	fwd = append(fwd, apfString("tcpip-forward")...)
	fwd = append(fwd, 1)
	fwd = append(fwd, apfString("")...)
	fwd = append(fwd, apfUint32(16992)...)
	writeAll(t, conn, fwd)
	expect(t, conn, transport.APFRequestSuccess)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	expect(t, conn, transport.APFKeepaliveOptionsRequest)
	require.NoError(t, conn.SetReadDeadline(time.Time{}))
	return conn
}

func writeAll(t *testing.T, w io.Writer, b []byte) {
	t.Helper()
	_, err := w.Write(b)
	require.NoError(t, err)
}

func expect(t *testing.T, conn net.Conn, want uint8) {
	t.Helper()
	msgType, _, err := transport.ReadMessage(conn)
	require.NoError(t, err)
	require.Equal(t, want, msgType)
}

func apfString(s string) []byte {
	return append(apfUint32(uint32(len(s))), s...)
}

func apfStringMsg(msgType uint8, s string) []byte {
	return append([]byte{msgType}, apfString(s)...)
}

func apfUint32(v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return b[:]
}

// intelGUID renders u as the wire GUID: the first three fields little-endian, the rest as-is.
func intelGUID(u uuid.UUID) []byte {
	return []byte{
		u[3], u[2], u[1], u[0],
		u[5], u[4],
		u[7], u[6],
		u[8], u[9], u[10], u[11], u[12], u[13], u[14], u[15],
	}
}
