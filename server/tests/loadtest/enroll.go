package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// EnrollOptions is one enrollment.
type EnrollOptions struct {
	BaseURL string
	// EnrollmentToken is minted through the admin API before the run and spent during it.
	EnrollmentToken string
	DeviceID        string
	// Hostname is the name this machine presents; empty falls back to the device id.
	Hostname string
	// PresentedAddress is the address this machine arrives from, counted per address by the
	// server. Empty presents nothing.
	PresentedAddress string
}

// IssuedCertificate is what an enrolled machine holds: its signed certificate and private key,
// plus the authority to verify the server with.
type IssuedCertificate struct {
	Certificate tls.Certificate
	CAPEM       string
	ServerAddr  string
}

// enrollTimeout bounds one enrollment, so a machine without an answer gives up its slot.
const enrollTimeout = 30 * time.Second

// EnrollAgent obtains a certificate for one machine through the public enrollment endpoint,
// sending only a signing request so the authority's key stays in the cluster.
func EnrollAgent(ctx context.Context, opts EnrollOptions) (*IssuedCertificate, error) {
	if err := CheckTarget(opts.BaseURL); err != nil {
		return nil, err
	}
	if opts.EnrollmentToken == "" {
		return nil, errors.New("enrollment needs a token; mint one through the admin API before the run")
	}
	if opts.DeviceID == "" {
		return nil, errors.New("enrollment needs a device id")
	}

	key, csrPEM, err := signingRequest(opts)
	if err != nil {
		return nil, err
	}

	response, err := postEnrollment(ctx, opts, csrPEM)
	if err != nil {
		return nil, err
	}
	if response.CertPEM == "" {
		return nil, errors.New("enrollment returned no certificate")
	}

	cert, err := tls.X509KeyPair([]byte(response.CertPEM), encodeKey(key))
	if err != nil {
		return nil, fmt.Errorf("enrollment returned a certificate that does not match the key sent for it: %w", err)
	}
	return &IssuedCertificate{
		Certificate: cert,
		CAPEM:       response.CAPEM,
		ServerAddr:  response.ServerAddr,
	}, nil
}

// enrollResponse is the shape the endpoint answers with.
type enrollResponse struct {
	CertPEM    string `json:"cert_pem"`
	CAPEM      string `json:"ca_pem"`
	ServerAddr string `json:"server_addr"`
}

// signingRequest builds a fresh key and the request that asks for it to be
// signed. The key stays here.
func signingRequest(opts EnrollOptions) (*ecdsa.PrivateKey, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate agent key: %w", err)
	}

	hostname := opts.Hostname
	if hostname == "" {
		hostname = opts.DeviceID
	}
	template := &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: opts.DeviceID},
		DNSNames: []string{hostname},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		return nil, nil, fmt.Errorf("build signing request: %w", err)
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}), nil
}

// ErrEnrollmentRefused is the server declining on purpose: a spent or invalid credential, or a
// rate past a ceiling it enforces.
var ErrEnrollmentRefused = errors.New("enrollment refused")

// ErrEnrollmentFailed is a machine that did not get in because the server broke or the harness
// asked wrongly; it belongs in the error rate.
var ErrEnrollmentFailed = errors.New("enrollment failed")

// deliberateRefusals are the statuses the endpoint chooses to answer with: an invalid token, a
// spent one and a rate past its ceiling. Any other status is a failure.
var deliberateRefusals = map[int]bool{
	http.StatusNotFound:        true,
	http.StatusGone:            true,
	http.StatusTooManyRequests: true,
}

func postEnrollment(ctx context.Context, opts EnrollOptions, csrPEM []byte) (*enrollResponse, error) {
	body, err := json.Marshal(map[string]string{"csr_pem": string(csrPEM)})
	if err != nil {
		return nil, fmt.Errorf("encode enrollment request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, enrollTimeout)
	defer cancel()

	url := opts.BaseURL + "/api/v1/enroll/" + opts.EnrollmentToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build enrollment request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	presentAddress(req, opts.PresentedAddress)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("enroll: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		outcome := ErrEnrollmentFailed
		if deliberateRefusals[resp.StatusCode] {
			outcome = ErrEnrollmentRefused
		}
		return nil, fmt.Errorf("%w with %d: %s", outcome, resp.StatusCode, bytes.TrimSpace(detail))
	}

	var decoded enrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode enrollment response: %w", err)
	}
	return &decoded, nil
}

// encodeKey writes the private key in memory for the TLS stack; it is never given a path on
// disk, where an artifact upload could collect it.
func encodeKey(key *ecdsa.PrivateKey) []byte {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}

// AgentTLSConfig presents the signed certificate and verifies the server against the authority
// the same enrollment returned.
func (c *IssuedCertificate) AgentTLSConfig() (*tls.Config, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(c.CAPEM)) {
		return nil, errors.New("enrollment returned an authority certificate that could not be parsed")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{c.Certificate},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"opengate"},
	}, nil
}
