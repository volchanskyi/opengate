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
)

// SignServer generates a CA-signed server certificate with localhost, 127.0.0.1 and extraDNS SANs.
func (m *Manager) SignServer(extraDNS ...string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate server key: %w", err)
	}

	template, err := localServerTemplate("OpenGate Server", x509.KeyUsageDigitalSignature, extraDNS...)
	if err != nil {
		return nil, err
	}

	return m.issueTLS(template, key, "server cert")
}

// SignMPS generates a CA-signed RSA 2048 certificate for the MPS server, as AMT firmware needs RSA.
func (m *Manager) SignMPS() (*tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate MPS key: %w", err)
	}

	template, err := localServerTemplate("OpenGate MPS", x509.KeyUsageDigitalSignature|x509.KeyUsageKeyEncipherment)
	if err != nil {
		return nil, err
	}

	return m.issueTLS(template, key, "MPS cert")
}

// localServerTemplate builds a server-auth template with localhost, 127.0.0.1 and extraDNS SANs.
func localServerTemplate(commonName string, usage x509.KeyUsage, extraDNS ...string) (*x509.Certificate, error) {
	template, err := leafTemplate(pkix.Name{CommonName: commonName}, usage, x509.ExtKeyUsageServerAuth)
	if err != nil {
		return nil, err
	}
	template.DNSNames = append([]string{"localhost"}, extraDNS...)
	template.IPAddresses = []net.IP{net.IPv4(127, 0, 0, 1)}
	return template, nil
}
