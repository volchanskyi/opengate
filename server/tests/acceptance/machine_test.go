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
	"github.com/volchanskyi/opengate/server/internal/testutil/machinecert"
)

// Machine is one enrolled endpoint holding a control stream, with an identity obtained from
// the public enrolment endpoint by signing a request for its own key.
type Machine struct {
	t       *testing.T
	product *Product

	// DeviceID is the number the server chose, which the certificate carries.
	DeviceID uuid.UUID
	// Hostname is the name shown in the device list.
	Hostname string
	// identity is the certificate and key the machine keeps across reconnects.
	identity *tls.Certificate
	// capabilities is what the machine registers with.
	capabilities []protocol.AgentCapability

	conn   *quic.Conn
	stream *quic.Stream
	codec  *protocol.Codec

	// writeMu keeps a pong and a sent message from interleaving on the stream.
	writeMu sync.Mutex

	// inbox holds the messages the product pushed; a single reader owns the stream.
	mu      sync.Mutex
	inbox   []*protocol.ControlMessage
	readErr error
}

// enrolReply is what the public enrolment endpoint answers with.
type enrolReply struct {
	CaPem      string    `json:"ca_pem"`
	CertPem    string    `json:"cert_pem"`
	DeviceID   uuid.UUID `json:"device_id"`
	ServerAddr string    `json:"server_addr"`
}

// Machine enrols a new machine with the given enrolment token and connects it; the server
// chooses its number.
func (p *Product) Machine(enrolmentToken, hostname string, capabilities ...protocol.AgentCapability) *Machine {
	p.t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(p.t, err)
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	require.NoError(p.t, err)
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	reply := p.enrol(enrolmentToken, string(csrPEM))
	require.NotEmpty(p.t, reply.CertPem, "an enrolment that signs nothing gives the machine no identity")

	certBlock, _ := pem.Decode([]byte(reply.CertPem))
	require.NotNil(p.t, certBlock)
	leaf, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(p.t, err)
	require.Equal(p.t, reply.DeviceID.String(), leaf.Subject.CommonName,
		"the reply names the number its certificate carries")

	machine := &Machine{
		t:            p.t,
		product:      p,
		DeviceID:     reply.DeviceID,
		Hostname:     hostname,
		identity:     &tls.Certificate{Certificate: [][]byte{certBlock.Bytes}, PrivateKey: key},
		capabilities: capabilities,
		codec:        &protocol.Codec{},
	}
	machine.connect(machine.identity)
	machine.register(capabilities)
	return machine
}

// Reconnect drops this connection and connects again with the certificate the machine kept, as
// an agent does after a restart, and registers.
func (m *Machine) Reconnect() *Machine {
	m.t.Helper()
	_ = m.conn.CloseWithError(0, "restarting")
	again := &Machine{
		t:            m.t,
		product:      m.product,
		DeviceID:     m.DeviceID,
		Hostname:     m.Hostname,
		identity:     m.identity,
		capabilities: m.capabilities,
		codec:        &protocol.Codec{},
	}
	again.connect(again.identity)
	again.register(again.capabilities)
	return again
}

// enrol posts a certificate request to the public enrolment endpoint and requires success.
func (p *Product) enrol(token, csrPEM string) enrolReply {
	p.t.Helper()

	attempt := p.enrolWith(token, csrPEM)
	require.Equalf(p.t, http.StatusOK, attempt.Status,
		"the enrolment token must be accepted: %s", attempt.Text())

	var out enrolReply
	require.NoError(p.t, json.Unmarshal(attempt.Body, &out))
	return out
}

// enrolAttempt tries to enrol with a token and returns the reply, refusals included.
func (p *Product) enrolAttempt(token string) Reply {
	p.t.Helper()
	return p.enrolWith(token, dummyCSR(p.t))
}

// enrolWith calls the public enrolment endpoint, which carries no operator credential.
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

// dummyCSR returns a well-formed certificate request for a throwaway identity.
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

// connect dials the QUIC listener and opens the control stream, writing first as the
// initiating side must for peer stream discovery.
func (m *Machine) connect(identity *tls.Certificate) {
	m.t.Helper()

	conn, err := quic.DialAddr(m.t.Context(), m.product.QUICAddr,
		m.product.machineTLS(identity),
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

// register announces the machine's hostname and capabilities and starts its reader.
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

// readLoop is the single goroutine that reads the stream and files pushed control messages.
func (m *Machine) readLoop() {
	for {
		frameType, payload, err := m.codec.ReadFrame(m.stream)
		if err != nil {
			m.mu.Lock()
			m.readErr = err
			m.mu.Unlock()
			return
		}
		if frameType == protocol.FramePing {
			// A live agent answers the server's ping, which tells a reconnect from a second machine.
			m.writeMu.Lock()
			_ = m.codec.WriteFrame(m.stream, protocol.FramePong, nil)
			m.writeMu.Unlock()
			continue
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

// Send writes one control message on the stream.
func (m *Machine) Send(msg *protocol.ControlMessage) {
	m.t.Helper()
	payload, err := m.codec.EncodeControl(msg)
	require.NoError(m.t, err)
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	require.NoError(m.t, m.codec.WriteFrame(m.stream, protocol.FrameControl, payload))
}

// Await waits for a pushed message of the given type and returns it, failing on timeout.
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

// Received reports without waiting whether a message of the given type has been pushed.
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

// Disconnect closes the connection without any goodbye on the stream.
func (m *Machine) Disconnect() {
	m.t.Helper()
	require.NoError(m.t, m.conn.CloseWithError(0, "machine left the network"))
}

// AwaitOnline blocks until the device row reads online.
func (m *Machine) AwaitOnline() {
	m.t.Helper()
	require.Eventually(m.t, func() bool {
		d, err := m.product.deviceRow(m.DeviceID)
		return err == nil && d.Status == db.StatusOnline
	}, eventually, poll, "the machine must appear online once it has registered")
}

// Renew answers the product's renewal offer as an agent does: a request from a new key, then the
// install notice once the certificate arrives. The machine keeps the new certificate.
func (m *Machine) Renew() {
	m.t.Helper()
	m.Await(protocol.MsgRenewCertificate)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(m.t, err)
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: m.DeviceID.String()},
	}, key)
	require.NoError(m.t, err)
	m.Send(&protocol.ControlMessage{Type: protocol.MsgCertificateSigningRequest, CSR: csr})
	issued := m.Await(protocol.MsgCertificateIssued)
	m.Send(&protocol.ControlMessage{Type: protocol.MsgCertificateInstalled})
	m.identity = &tls.Certificate{Certificate: [][]byte{issued.Cert}, PrivateKey: key}
}

// machineTLS is the configuration a machine holding identity dials with, verifying the server
// on the product's own clock.
func (p *Product) machineTLS(identity *tls.Certificate) *tls.Config {
	cfg := machinecert.ClientTLS(p.assembly.Cert, identity)
	cfg.Time = p.clock.Now
	return cfg
}

// returning is a machine coming back with the certificate it kept: dialled, greeted and
// registered, its pushed messages collected, and how the server closed it, if it did.
type returning struct {
	conn  *quic.Conn
	inbox chan *protocol.ControlMessage
}

// returnWith connects as a machine holding identity, on the full or the quick path, and registers.
func (p *Product) returnWith(identity *tls.Certificate, hostname string, quick bool) *returning {
	p.t.Helper()
	conn, err := quic.DialAddr(p.t.Context(), p.QUICAddr, p.machineTLS(identity),
		&quic.Config{MaxIdleTimeout: 10 * time.Second})
	require.NoError(p.t, err)
	p.t.Cleanup(func() { _ = conn.CloseWithError(0, "test done") })
	stream, err := conn.OpenStreamSync(p.t.Context())
	require.NoError(p.t, err)
	if quick {
		_, err = stream.Write(protocol.EncodeSkipAuth(p.assembly.Cert.IssuerHash()))
		require.NoError(p.t, err)
	} else {
		var nonce [32]byte
		_, err = rand.Read(nonce[:])
		require.NoError(p.t, err)
		_, err = stream.Write(protocol.EncodeAgentHello(nonce, sha512.Sum384(identity.Certificate[0])))
		require.NoError(p.t, err)
		_, _ = io.ReadFull(stream, make([]byte, 81))
	}
	codec := &protocol.Codec{}
	payload, err := codec.EncodeControl(&protocol.ControlMessage{
		Type: protocol.MsgAgentRegister, Capabilities: []protocol.AgentCapability{protocol.CapTerminal},
		Hostname: hostname, OS: "linux", Arch: "amd64", Version: "1.0.0",
	})
	require.NoError(p.t, err)
	_ = codec.WriteFrame(stream, protocol.FrameControl, payload)

	r := &returning{conn: conn, inbox: make(chan *protocol.ControlMessage, 32)}
	go func() {
		defer close(r.inbox)
		for {
			frameType, payload, err := codec.ReadFrame(stream)
			if err != nil {
				return
			}
			if frameType == protocol.FrameControl {
				if msg, err := codec.DecodeControl(payload); err == nil {
					r.inbox <- msg
				}
			}
		}
	}()
	return r
}

// awaitMessage waits for a pushed message of the given type, failing if the connection ends first.
func (r *returning) awaitMessage(t *testing.T, want protocol.ControlMessageType) *protocol.ControlMessage {
	t.Helper()
	deadline := time.After(eventually)
	for {
		select {
		case msg, ok := <-r.inbox:
			require.True(t, ok, "the connection ended before a %s arrived", want)
			if msg.Type == want {
				return msg
			}
		case <-deadline:
			t.Fatalf("the product never sent a %s", want)
		}
	}
}

// closeCode waits for the server to end the connection and returns the code it closed with.
func (r *returning) closeCode(t *testing.T) uint64 {
	t.Helper()
	select {
	case <-r.conn.Context().Done():
	case <-time.After(eventually + 5*time.Second):
		t.Fatal("the product held the connection open")
	}
	var appErr *quic.ApplicationError
	require.ErrorAs(t, context.Cause(r.conn.Context()), &appErr)
	return uint64(appErr.ErrorCode)
}
