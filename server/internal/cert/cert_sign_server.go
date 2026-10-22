package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net"
	"time"
)

// serverRefreshWindow is how close to its expiry the server's own certificate is re-signed.
const serverRefreshWindow = 30 * 24 * time.Hour

// SignServer generates a server certificate with localhost, 127.0.0.1 and extraDNS SANs, followed
// by the chain the trust set names.
func (m *Manager) SignServer(extraDNS ...string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate server key: %w", err)
	}
	template, err := localServerTemplate(m.issuer, m.now(), "OpenGate Server", x509.KeyUsageDigitalSignature, extraDNS...)
	if err != nil {
		return nil, err
	}
	return issueTLS(&template, m.issuer, m.issuerKey, key, m.chain, "server cert")
}

// SignMPS generates an RSA 2048 certificate for the MPS server, as AMT firmware needs RSA, signed
// by the master key the firmware was provisioned with.
func (m *Manager) SignMPS() (*tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate MPS key: %w", err)
	}
	template, err := localServerTemplate(m.mpsIssuer, m.now(), "OpenGate MPS",
		x509.KeyUsageDigitalSignature|x509.KeyUsageKeyEncipherment)
	if err != nil {
		return nil, err
	}
	return issueTLS(&template, m.mpsIssuer, m.mpsIssuerKey, key, nil, "MPS cert")
}

// serverCertificate serves the cached server certificate, signing a new one within
// serverRefreshWindow of its expiry so a long-running process never presents an expired one.
func (m *Manager) serverCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.serverMu.Lock()
	defer m.serverMu.Unlock()
	return m.refresh(&m.serverTLS, func() (*tls.Certificate, error) { return m.SignServer(m.extraDNS...) })
}

// mpsCertificate is serverCertificate for the MPS listener.
func (m *Manager) mpsCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mpsMu.Lock()
	defer m.mpsMu.Unlock()
	return m.refresh(&m.mpsTLS, m.SignMPS)
}

func (m *Manager) refresh(cached **tls.Certificate, sign func() (*tls.Certificate, error)) (*tls.Certificate, error) {
	if *cached != nil && m.now().Add(serverRefreshWindow).Before((*cached).Leaf.NotAfter) {
		return *cached, nil
	}
	fresh, err := sign()
	if err != nil {
		return nil, err
	}
	*cached = fresh
	return fresh, nil
}

// localServerTemplate builds a server-auth template with localhost, 127.0.0.1 and extraDNS SANs,
// returned by value like leafTemplate.
func localServerTemplate(issuer *x509.Certificate, now time.Time, commonName string, usage x509.KeyUsage, extraDNS ...string) (x509.Certificate, error) {
	template, err := leafTemplate(issuer, now, pkix.Name{CommonName: commonName}, usage, x509.ExtKeyUsageServerAuth)
	if err != nil {
		return x509.Certificate{}, err
	}
	template.DNSNames = append([]string{"localhost"}, extraDNS...)
	template.IPAddresses = []net.IP{net.IPv4(127, 0, 0, 1)}
	return template, nil
}
