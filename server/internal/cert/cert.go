// Package cert holds the master keys the server trusts, issues device and server certificates,
// and classifies the certificate a machine presents.
package cert

import (
	"crypto"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"
	"time"
)

// DefaultExpiredGrace is how long past its expiry a certificate is still admitted to renew.
const DefaultExpiredGrace = 90 * 24 * time.Hour

// Manager holds the trust set and signs certificates with the issuing master key.
type Manager struct {
	now func() time.Time

	issuer    *x509.Certificate
	issuerKey crypto.Signer
	// authorities is every trusted master key, the issuer first.
	authorities []Authority
	// chain follows the server's own certificate: the cross-signed issuer during a changeover.
	chain [][]byte
	// bundlePEM is the issuer and every accepted master key, the set a machine is told to trust.
	bundlePEM []byte

	mpsIssuer    *x509.Certificate
	mpsIssuerKey crypto.Signer

	compromisedSince time.Time
	expiredGrace     time.Duration
	extraDNS         []string

	serverMu  sync.Mutex
	serverTLS *tls.Certificate
	mpsMu     sync.Mutex
	mpsTLS    *tls.Certificate
}

// Option configures a Manager.
type Option func(*options)

type options struct {
	clock            func() time.Time
	mustExist        bool
	compromisedSince time.Time
	expiredGrace     time.Duration
}

// WithClock sets the clock every issuance, renewal and expiry decision reads.
func WithClock(now func() time.Time) Option {
	return func(o *options) { o.clock = now }
}

// WithMustExist refuses to start without the issuing master key, which is never generated then.
func WithMustExist() Option {
	return func(o *options) { o.mustExist = true }
}

// WithCompromisedSince dates the leak a vouched-only master key was dropped for.
func WithCompromisedSince(t time.Time) Option {
	return func(o *options) { o.compromisedSince = t }
}

// WithExpiredGrace sets how long past expiry a certificate is still admitted to renew.
func WithExpiredGrace(d time.Duration) Option {
	return func(o *options) { o.expiredGrace = d }
}

// NewManager loads the trust set from the role files in dir, generating an issuing master key
// there when none exists and WithMustExist is absent.
func NewManager(dir string, opts ...Option) (*Manager, error) {
	o := options{clock: time.Now, expiredGrace: DefaultExpiredGrace}
	for _, opt := range opts {
		opt(&o)
	}
	set, err := LoadTrustSet(dir, o.mustExist, o.clock)
	if err != nil {
		return nil, err
	}
	if len(set.vouchedOnly) > 0 && o.compromisedSince.IsZero() {
		return nil, errors.New("a vouched-only master key needs the date of the compromise")
	}
	return newManager(set, o)
}

func newManager(set *TrustSet, o options) (*Manager, error) {
	m := &Manager{
		now:              o.clock,
		issuer:           set.issuer,
		issuerKey:        set.issuerKey,
		authorities:      set.authorities(),
		chain:            set.chain,
		mpsIssuer:        set.mpsIssuer,
		mpsIssuerKey:     set.mpsIssuerKey,
		compromisedSince: o.compromisedSince,
		expiredGrace:     o.expiredGrace,
	}
	for _, a := range m.authorities {
		if a.Role == RoleIssuer || a.Role == RoleAccepted {
			m.bundlePEM = append(m.bundlePEM,
				pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.Cert.Raw})...)
		}
	}
	return m, nil
}

// Now is the server's clock, which decides renewal, expiry and issuance.
func (m *Manager) Now() time.Time {
	return m.now()
}

// CACert returns the issuing master key's certificate.
func (m *Manager) CACert() *x509.Certificate {
	return m.issuer
}

// CACertPEM returns the trust bundle: the issuing master key and every accepted one.
func (m *Manager) CACertPEM() []byte {
	return m.bundlePEM
}

// IssuerHash is the SHA-384 of the issuing master key, which ServerHello announces.
func (m *Manager) IssuerHash() [48]byte {
	return sha512.Sum384(m.issuer.Raw)
}

// TrustedHashes is the SHA-384 of every trusted master key, any of which the fast path accepts.
func (m *Manager) TrustedHashes() [][48]byte {
	hashes := make([][48]byte, len(m.authorities))
	for i, a := range m.authorities {
		hashes[i] = a.Hash
	}
	return hashes
}

// CompromisedSince is the date a vouched-only master key leaked; zero when none is.
func (m *Manager) CompromisedSince() time.Time {
	return m.compromisedSince
}

// Authorities lists every trusted master key with its role.
func (m *Manager) Authorities() []Authority {
	return append([]Authority(nil), m.authorities...)
}

// ServerTLSConfig returns the machine-facing TLS configuration: a client certificate is required
// and classified in VerifyConnection, which runs on resumed handshakes as well.
func (m *Manager) ServerTLSConfig(extraDNS ...string) (*tls.Config, error) {
	m.serverMu.Lock()
	m.extraDNS = extraDNS
	m.serverMu.Unlock()
	if _, err := m.serverCertificate(nil); err != nil {
		return nil, fmt.Errorf("sign server cert: %w", err)
	}
	return &tls.Config{
		GetCertificate: m.serverCertificate,
		ClientAuth:     tls.RequireAnyClientCert,
		// The pool is a hint to the client; Classify decides.
		ClientCAs: m.pool(),
		VerifyConnection: func(cs tls.ConnectionState) error {
			_, err := m.Classify(cs.PeerCertificates)
			return err
		},
		Time:       m.now,
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"opengate"},
	}, nil
}

// MPSTLSConfig returns a tls.Config for the MPS server, with the TLS 1.2 minimum AMT 11.0+ speaks.
func (m *Manager) MPSTLSConfig() (*tls.Config, error) {
	if _, err := m.mpsCertificate(nil); err != nil {
		return nil, fmt.Errorf("sign MPS cert: %w", err)
	}
	return &tls.Config{
		GetCertificate: m.mpsCertificate,
		ClientAuth:     tls.NoClientCert,
		MinVersion:     tls.VersionTLS12,
	}, nil
}

// pool holds every trusted master key.
func (m *Manager) pool() *x509.CertPool {
	pool := x509.NewCertPool()
	for _, a := range m.authorities {
		pool.AddCert(a.Cert)
	}
	return pool
}
