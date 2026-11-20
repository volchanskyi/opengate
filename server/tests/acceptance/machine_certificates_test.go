package acceptance

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func csrNaming(t *testing.T, cn string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: cn},
	}, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

const day = 24 * time.Hour

func TestEnrolmentGivesEveryMachineANumberOfTheServersChoosing(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	contoso := product.arrangeCustomer("Contoso")
	admin := product.Administrator(contoso)
	token := admin.mintEnrolmentToken("Head Office").Token
	existing := product.Machine(token, "contoso-reception-pc")
	existing.AwaitOnline()

	// A join code asking for the existing machine's number is given another.
	reply := product.enrol(token, csrNaming(t, existing.DeviceID.String()))
	assert.NotEqual(t, existing.DeviceID, reply.DeviceID)

	shown := admin.listed(existing)
	assert.Equal(t, statusOnline, shown.Status, "the existing machine stays online")
	assert.Equal(t, "contoso-reception-pc", shown.Hostname, "and unchanged")
}

func TestAMachineRenewsItsCertificateAndStaysTheSameMachine(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	admin, machine := contosoMachine(t, product, "Head Office", "contoso-workstation")
	old := machine.identity

	product.Advance(61 * day)
	machine = machine.Reconnect()
	machine.Renew()
	machine = machine.Reconnect()
	machine.AwaitOnline()

	listed := admin.devices()
	require.Len(t, listed, 1, "a renewed machine is the same machine")
	assert.Equal(t, machine.DeviceID, listed[0].ID)
	assert.Equal(t, uint64(protocol.CloseCertificateRefused),
		product.returnWith(old, machine.Hostname, false).closeCode(t), "its old certificate is refused")
}

func TestAMachineWithAnExpiredCertificateIsLetInOnlyToRenew(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	admin, machine := contosoMachine(t, product, "Head Office", "contoso-spare-laptop")
	machine.Disconnect()
	product.awaitShown(machine.DeviceID, statusOffline, "a machine that left the network is shown offline")

	product.Advance(100 * day)
	machine = machine.Reconnect()
	machine.Send(&protocol.ControlMessage{Type: protocol.MsgHardwareReport, CPUModel: "Contoso spare", CPUCores: 4})
	// The certificate is issued after the report on the one stream, so the report was handled.
	machine.Renew()
	renewing, listed := product.shown(machine.DeviceID)
	require.True(t, listed)
	assert.Equal(t, statusOffline, renewing.Status, "an expired machine is not shown online")
	assert.NotEqual(t, http.StatusOK, admin.Get("/api/v1/devices/"+machine.DeviceID.String()+"/hardware").Status,
		"and the report it sent stored nothing")

	machine = machine.Reconnect()
	machine.AwaitOnline()
}

func TestACertificateTheServerNeverIssuedIsRefused(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	product.arrangeCustomer("Contoso")
	block, _ := pem.Decode([]byte(csrNaming(t, "")))
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	require.NoError(t, err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	csr.PublicKey = &key.PublicKey

	// Signed by the product's own master key, but never recorded as issued.
	forgedDER, err := product.assembly.Cert.IssueDevice(csr, uuid.New())
	require.NoError(t, err)
	forged := &tls.Certificate{Certificate: [][]byte{forgedDER}, PrivateKey: key}

	assert.Equal(t, uint64(protocol.CloseCertificateRefused),
		product.returnWith(forged, "forged", false).closeCode(t))
}

func TestAStolenOldKeyStopsWorkingOnceTheMachineRenews(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	_, machine := contosoMachine(t, product, "Head Office", "contoso-ceo-laptop")
	stolen := machine.identity
	machine.Disconnect()

	// Both copies return with the expired certificate and are each let in only to renew.
	product.Advance(100 * day)
	thief := product.returnWith(stolen, "attacker", false)
	thief.awaitMessage(t, protocol.MsgRenewCertificate)
	machine = machine.Reconnect()
	machine.Renew()

	assert.Equal(t, uint64(protocol.CloseCertificateRefused), thief.closeCode(t),
		"the copy is disconnected once the real machine installs its renewal")
	assert.Equal(t, uint64(protocol.CloseCertificateRefused),
		product.returnWith(stolen, "attacker", false).closeCode(t), "and refused afterwards")
}

func TestTwoMachinesSharingAnIdentityAreShownToTheTechnician(t *testing.T) {
	t.Parallel()

	product := newProduct(t)
	admin, original := contosoMachine(t, product, "Classroom image", "classroom-pc")

	// A second PC cloned from the same image connects while the first still answers.
	product.returnWith(original.identity, "classroom-pc", false).awaitMessage(t, protocol.MsgTrustBundle)

	var page struct {
		IdentitySharedAt        *time.Time `json:"identity_shared_at"`
		IdentitySharedAddresses []string   `json:"identity_shared_addresses"`
	}
	require.Eventually(t, func() bool {
		admin.Get("/api/v1/devices/" + original.DeviceID.String()).Into(&page)
		return page.IdentitySharedAt != nil
	}, eventually, poll, "the device page warns that one identity is on two machines")
	assert.Len(t, page.IdentitySharedAddresses, 2)
	assert.Len(t, admin.devices(), 1, "the console still lists one device")
}
