package acceptance

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// Machine is one enrolled endpoint holding a control stream. It reaches the
// product only through that stream, and it gets its identity the way a real
// machine does: it generates a key, asks the public enrolment endpoint to sign
// a request for it, and dials with the certificate that comes back. No private
// key ever leaves this process and no test-only bypass exists.
type Machine struct {
	t       *testing.T
	product *Product

	// DeviceID is the identity the certificate carries and the product files
	// the machine under.
	DeviceID uuid.UUID
	// Hostname is the name a technician sees in the device list.
	Hostname string

	conn   *quic.Conn
	stream *quic.Stream
	codec  *protocol.Codec

	// inbox holds what the product has pushed down the stream. One reader owns
	// the stream, so a test can wait for a message without racing another wait.
	mu      sync.Mutex
	inbox   []*protocol.ControlMessage
	readErr error
}

// enrolReply is what the public enrolment endpoint answers with.
type enrolReply struct {
	CaPem      string `json:"ca_pem"`
	CertPem    string `json:"cert_pem"`
	ServerAddr string `json:"server_addr"`
}

// Machine enrols a new machine with the given enrolment token and connects it.
// This is the whole path a real endpoint walks: mint, request, sign, dial,
// register.
func (p *Product) Machine(enrolmentToken, hostname string, capabilities ...protocol.AgentCapability) *Machine {
	p.t.Helper()
	return p.MachineWithIdentity(enrolmentToken, uuid.New(), hostname, capabilities...)
}

// MachineWithIdentity enrols a machine that already knows who it is. A
// rebuilt endpoint keeps the identity it was installed with and asks for a
// fresh certificate against it, which is how it comes back as itself rather
// than as a second row beside itself.
func (p *Product) MachineWithIdentity(
	enrolmentToken string, deviceID uuid.UUID, hostname string, capabilities ...protocol.AgentCapability,
) *Machine {
	p.t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(p.t, err)

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: deviceID.String()},
	}, key)
	require.NoError(p.t, err)
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	reply := p.enrol(enrolmentToken, string(csrPEM))
	require.NotEmpty(p.t, reply.CertPem, "an enrolment that signs nothing gives the machine no identity")

	certBlock, _ := pem.Decode([]byte(reply.CertPem))
	require.NotNil(p.t, certBlock)

	machine := &Machine{
		t:        p.t,
		product:  p,
		DeviceID: deviceID,
		Hostname: hostname,
		codec:    &protocol.Codec{},
	}
	machine.connect(&tls.Certificate{Certificate: [][]byte{certBlock.Bytes}, PrivateKey: key})
	machine.register(capabilities)
	return machine
}

// enrol posts a certificate request to the public enrolment endpoint and
// insists it succeeded.
func (p *Product) enrol(token, csrPEM string) enrolReply {
	p.t.Helper()

	attempt := p.enrolWith(token, csrPEM)
	require.Equalf(p.t, http.StatusOK, attempt.Status,
		"the enrolment token must be accepted: %s", attempt.Text())

	var out enrolReply
	require.NoError(p.t, json.Unmarshal(attempt.Body, &out))
	return out
}

// enrolAttempt asks to enrol with a token and no certificate request, which is
// exactly what the install script does to check a token before it commits to
// anything. It returns whatever came back, refusals included.
func (p *Product) enrolAttempt(token string) Reply {
	p.t.Helper()
	return p.enrolWith(token, dummyCSR(p.t))
}

// enrolWith is the public enrolment endpoint as a machine reaches it:
// deliberately unauthenticated, because a machine being installed holds no
// operator credential — only the token somebody pasted into its install
// command.
func (p *Product) enrolWith(token, csrPEM string) Reply {
	p.t.Helper()

	body := `{"csr_pem":` + quoteJSON(csrPEM) + `}`
	req, err := http.NewRequestWithContext(p.t.Context(), http.MethodPost,
		p.HTTP.URL+"/api/v1/enroll/"+token, stringReader(body))
	require.NoError(p.t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.HTTP.Client().Do(req)
	require.NoError(p.t, err)
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	require.NoError(p.t, err)
	return Reply{t: p.t, Status: resp.StatusCode, Body: payload}
}

// dummyCSR is a well-formed certificate request for an identity nothing will
// ever use. A refusal must come from the token, not from a malformed request.
func dummyCSR(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: uuid.NewString()},
	}, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

// connect dials the machine-facing door and opens the control stream. The
// machine opens the stream and writes first, which is what RFC 9000 stream
// discovery requires of the side that initiates.
func (m *Machine) connect(identity *tls.Certificate) {
	m.t.Helper()

	conn, err := quic.DialAddr(m.t.Context(), m.product.QUICAddr,
		m.product.assembly.Cert.AgentTLSConfig(identity),
		&quic.Config{MaxIdleTimeout: 30 * time.Second})
	require.NoError(m.t, err)
	m.t.Cleanup(func() { _ = conn.CloseWithError(0, "test done") })
	m.conn = conn

	stream, err := conn.OpenStreamSync(m.t.Context())
	require.NoError(m.t, err)
	require.Zerof(m.t, int64(stream.StreamID())%2, "the machine opens the control stream, so its id is even")
	m.stream = stream

	certHash := sha512.Sum384(identity.Certificate[0])
	var nonce [32]byte
	_, err = rand.Read(nonce[:])
	require.NoError(m.t, err)
	_, err = stream.Write(protocol.EncodeAgentHello(nonce, certHash))
	require.NoError(m.t, err)

	hello := make([]byte, 81)
	_, err = io.ReadFull(stream, hello)
	require.NoError(m.t, err)
	require.Equal(m.t, byte(protocol.MsgServerHello), hello[0], "the product must greet the machine back")
}

// register tells the product what this machine is and what it can do.
func (m *Machine) register(capabilities []protocol.AgentCapability) {
	m.t.Helper()

	if len(capabilities) == 0 {
		capabilities = []protocol.AgentCapability{
			protocol.CapTerminal, protocol.CapHardwareInventory, protocol.CapDeviceLogs,
		}
	}
	m.Send(&protocol.ControlMessage{
		Type:         protocol.MsgAgentRegister,
		Capabilities: capabilities,
		Hostname:     m.Hostname,
		OS:           "linux",
		Arch:         "amd64",
		Version:      "1.0.0",
	})
	go m.readLoop()
}

// readLoop is the machine's own reader: one goroutine owns the stream and
// files everything the product pushes, so a test can wait for a message
// without two waits racing for the same bytes.
func (m *Machine) readLoop() {
	for {
		frameType, payload, err := m.codec.ReadFrame(m.stream)
		if err != nil {
			m.mu.Lock()
			m.readErr = err
			m.mu.Unlock()
			return
		}
		if frameType != protocol.FrameControl {
			continue
		}
		msg, err := m.codec.DecodeControl(payload)
		if err != nil {
			m.mu.Lock()
			m.readErr = err
			m.mu.Unlock()
			return
		}
		m.mu.Lock()
		m.inbox = append(m.inbox, msg)
		m.mu.Unlock()
	}
}

// Send writes one control message the way a machine does.
func (m *Machine) Send(msg *protocol.ControlMessage) {
	m.t.Helper()
	payload, err := m.codec.EncodeControl(msg)
	require.NoError(m.t, err)
	require.NoError(m.t, m.codec.WriteFrame(m.stream, protocol.FrameControl, payload))
}

// Await waits for the product to push a message of the given type and returns
// it. It fails the test rather than blocking for ever, naming the message it
// was waiting for.
func (m *Machine) Await(msgType protocol.ControlMessageType) *protocol.ControlMessage {
	m.t.Helper()

	var found *protocol.ControlMessage
	require.Eventuallyf(m.t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, msg := range m.inbox {
			if msg.Type == msgType {
				found = msg
				return true
			}
		}
		return false
	}, eventually, poll, "the product never pushed a %s to the machine", msgType)
	return found
}

// Received reports whether the product has pushed a message of the given type
// at any point. Unlike Await it does not wait, so it states a negative.
func (m *Machine) Received(msgType protocol.ControlMessageType) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range m.inbox {
		if msg.Type == msgType {
			return true
		}
	}
	return false
}

// Disconnect drops the machine off the network the way a laptop closing its
// lid does: no goodbye, just silence.
func (m *Machine) Disconnect() {
	m.t.Helper()
	require.NoError(m.t, m.conn.CloseWithError(0, "machine left the network"))
}

// AwaitOnline blocks until the product shows the machine as online, which is
// the state a technician's device list reads.
func (m *Machine) AwaitOnline() {
	m.t.Helper()
	require.Eventually(m.t, func() bool {
		d, err := m.product.deviceRow(m.DeviceID)
		return err == nil && d.Status == db.StatusOnline
	}, eventually, poll, "the machine must appear online once it has registered")
}

// tryReconnect walks the whole return path for a machine that already exists —
// certificate request, dial, greeting, register — and reports the first thing
// that refused it instead of failing the test. It is how an outcome states
// that an identity is no longer trusted.
func (p *Product) tryReconnect(machine *Machine, enrolmentToken string) error {
	p.t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: machine.DeviceID.String()},
	}, key)
	if err != nil {
		return err
	}
	attempt := p.enrolWith(enrolmentToken, string(pem.EncodeToMemory(
		&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})))
	if attempt.Status != http.StatusOK {
		return fmt.Errorf("enrolment refused with %d", attempt.Status)
	}
	var reply enrolReply
	if err := json.Unmarshal(attempt.Body, &reply); err != nil {
		return err
	}
	certBlock, _ := pem.Decode([]byte(reply.CertPem))
	if certBlock == nil {
		return errors.New("enrolment signed nothing")
	}

	ctx, cancel := context.WithTimeout(p.t.Context(), 5*time.Second)
	defer cancel()
	conn, err := quic.DialAddr(ctx, p.QUICAddr,
		p.assembly.Cert.AgentTLSConfig(&tls.Certificate{
			Certificate: [][]byte{certBlock.Bytes}, PrivateKey: key,
		}),
		&quic.Config{MaxIdleTimeout: 5 * time.Second})
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseWithError(0, "reconnect attempt done") }()

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return err
	}
	certHash := sha512.Sum384(certBlock.Bytes)
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	if _, err := stream.Write(protocol.EncodeAgentHello(nonce, certHash)); err != nil {
		return err
	}
	if err := stream.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	hello := make([]byte, 81)
	if _, err := io.ReadFull(stream, hello); err != nil {
		return err
	}

	codec := &protocol.Codec{}
	payload, err := codec.EncodeControl(&protocol.ControlMessage{
		Type:         protocol.MsgAgentRegister,
		Capabilities: []protocol.AgentCapability{protocol.CapTerminal},
		Hostname:     machine.Hostname, OS: "linux", Arch: "amd64", Version: "1.0.0",
	})
	if err != nil {
		return err
	}
	return codec.WriteFrame(stream, protocol.FrameControl, payload)
}
