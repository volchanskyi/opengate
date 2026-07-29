package transport_test

// This file lives in the external test package on purpose. Registration is
// exactly where the tenancy defect hid: mps_test.go substituted a writer that
// bypassed dbtx.Scoped, so the production path — amt.PostgresAMTDevices, which
// refuses a context with no tenant — was never exercised. Package transport
// cannot import amt (amt.Service holds a *transport.Server), but transport_test
// can, so these tests drive the real repositories end to end.
//
// The CIRA client side is written out here rather than shared with mps_test.go:
// an independent encoder is what makes a wire-protocol test worth having.

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

// newLinkEnv starts an MPS server wired to the production Postgres adapters.
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

// seedDeviceWithSystemUUID creates a device in orgCtx's tenant whose hardware
// row reports systemUUID — the state a registered agent leaves behind.
func seedDeviceWithSystemUUID(t *testing.T, orgCtx context.Context, env *linkEnv, systemUUID uuid.UUID) *device.Device {
	t.Helper()
	owner := testutil.SeedUser(t, orgCtx, env.store)
	group := testutil.SeedGroup(t, orgCtx, env.store, owner.ID)
	dev := testutil.SeedDevice(t, orgCtx, env.store, group.ID)
	available := true
	require.NoError(t, env.hardware.Upsert(orgCtx, &device.Hardware{
		DeviceID:     dev.ID,
		CPUModel:     "Intel Core i7-12700K",
		SystemUUID:   &systemUUID,
		AMTAvailable: &available,
		AMTVersion:   "16.1.30.2260",
	}))
	return dev
}

// amtRow reads the persisted connection state for amtUUID inside orgCtx's
// tenant, or nil when no row exists there.
func amtRow(t *testing.T, orgCtx context.Context, env *linkEnv, amtUUID uuid.UUID) *db.AMTDevice {
	t.Helper()
	tenant, ok := dbtx.TenantFromContext(orgCtx)
	require.True(t, ok, "the reader needs a tenant to scope by")

	var row db.AMTDevice
	err := env.store.DB().QueryRowContext(orgCtx,
		`SELECT uuid, device_id, status FROM amt_devices WHERE org_id = $1 AND uuid = $2`,
		tenant.OrgID, amtUUID).Scan(&row.UUID, &row.DeviceID, &row.Status)
	if err != nil {
		return nil
	}
	return &row
}

// TestCIRAConnectPersistsUnderTheDeviceOrg is the repair this change exists for:
// a CIRA connect whose system UUID matches a managed device must write a row
// carrying that device's organization, through the real repository.
func TestCIRAConnectPersistsUnderTheDeviceOrg(t *testing.T) {
	env := newLinkEnv(t)
	orgB := uuid.New()
	testutil.EnsureOrganization(t, context.Background(), env.store, orgB, "Tenant "+orgB.String()[:8])
	orgCtx := dbtx.WithTenant(context.Background(), orgB, false)

	amtUUID := uuid.New()
	dev := seedDeviceWithSystemUUID(t, orgCtx, env, amtUUID)

	conn := dialCIRA(t, env, amtUUID)
	t.Cleanup(func() { _ = conn.Close() })

	require.Eventually(t, func() bool { return env.srv.GetConn(amtUUID) != nil },
		5*time.Second, 10*time.Millisecond, "server should register the CIRA connection")
	require.Eventually(t, func() bool {
		row := amtRow(t, orgCtx, env, amtUUID)
		return row != nil && row.Status == db.StatusOnline
	}, 5*time.Second, 10*time.Millisecond, "the AMT row should be persisted online in the device's org")

	row := amtRow(t, orgCtx, env, amtUUID)
	require.NotNil(t, row)
	assert.Equal(t, dev.ID, row.DeviceID, "the row should point at the device that reported this system UUID")

	// Disconnect marks it offline — which also needs the resolved tenant.
	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool {
		row := amtRow(t, orgCtx, env, amtUUID)
		return row != nil && row.Status == db.StatusOffline
	}, 5*time.Second, 10*time.Millisecond, "disconnect should mark the row offline in the device's org")
}

// TestCIRAConnectWithNoDevicePersistsNothing covers the locked decision: an AMT
// box with no managed device has no organization to live in, so the connection
// is held in memory and nothing is written.
func TestCIRAConnectWithNoDevicePersistsNothing(t *testing.T) {
	env := newLinkEnv(t)
	orgCtx := dbtx.WithDefaultTenant(context.Background(), true)
	amtUUID := uuid.New()

	conn := dialCIRA(t, env, amtUUID)
	t.Cleanup(func() { _ = conn.Close() })

	require.Eventually(t, func() bool { return env.srv.GetConn(amtUUID) != nil },
		5*time.Second, 10*time.Millisecond, "the unmatched connection should still be held in memory")

	// Give registration room to have written something it should not have.
	assert.Never(t, func() bool { return amtRow(t, orgCtx, env, amtUUID) != nil },
		time.Second, 50*time.Millisecond, "an unmatched CIRA connection must persist nothing")
	assert.Equal(t, 1, env.srv.ConnectedDeviceCount(), "the connection stays live for a later keepalive to adopt")
}

// dialCIRA opens a TLS connection and walks the full CIRA handshake from the AMT
// device side, returning once the server has sent its keepalive options.
//
// The MPS certificate is CA-signed with a localhost SAN, so the client verifies
// it against the server's own CA rather than skipping verification — the
// handshake under test is the one a real AMT device performs.
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

	// ProtocolVersion: type, major/minor/trigger, then the UUID in Intel's
	// mixed-endian layout.
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
	fwd = append(fwd, 1) // want_reply
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

// apfString encodes one APF string: 4-byte big-endian length, then the bytes.
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

// intelGUID renders a UUID in the mixed-endian layout AMT puts on the wire: the
// first three groups little-endian, the rest as-is.
func intelGUID(u uuid.UUID) []byte {
	return []byte{
		u[3], u[2], u[1], u[0],
		u[5], u[4],
		u[7], u[6],
		u[8], u[9], u[10], u[11], u[12], u[13], u[14], u[15],
	}
}
