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

// Machine is one enrolled endpoint holding a control stream, with an identity obtained from
// the public enrolment endpoint by signing a request for its own key.
type Machine struct {
	t       *testing.T
	product *Product

	// DeviceID is the identity the certificate carries.
	DeviceID uuid.UUID
	// Hostname is the name shown in the device list.
	Hostname string

	conn   *quic.Conn
	stream *quic.Stream
	codec  *protocol.Codec

	// inbox holds the messages the product pushed; a single reader owns the stream.
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
func (p *Product) Machine(enrolmentToken, hostname string, capabilities ...protocol.AgentCapability) *Machine {
	p.t.Helper()
	return p.MachineWithIdentity(enrolmentToken, uuid.New(), hostname, capabilities...)
}

// MachineWithIdentity enrols a machine under an existing device identity with a fresh certificate.
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

// tryReconnect repeats enrolment, dial, greeting and registration for an existing machine and
// returns the first refusal.
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
