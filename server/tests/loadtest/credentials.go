package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/cert"
)

// agentCredentials issues the TLS material one simulated machine dials with.
type agentCredentials interface {
	forAgent(ctx context.Context, plan tenantAgent) (*tls.Config, error)
}

// newAgentCredentials picks the source: the server signs when an enrollment URL is given, and
// the harness signs against a local authority in dataDir otherwise.
func newAgentCredentials(dataDir, enrollURL, enrollToken string) (agentCredentials, error) {
	if enrollURL != "" {
		if enrollToken == "" {
			return nil, errors.New("enrolling needs a token; mint one through the admin API before the run")
		}
		if err := CheckTarget(enrollURL); err != nil {
			return nil, err
		}
		return enrolledCredentials{baseURL: enrollURL, token: enrollToken}, nil
	}

	if dataDir == "" {
		return nil, errors.New("without an enrollment URL the harness needs a data directory holding a certificate authority")
	}
	manager, err := cert.NewManager(dataDir)
	if err != nil {
		return nil, fmt.Errorf("cert manager: %w", err)
	}
	return localCredentials{manager: manager}, nil
}

// enrolledCredentials asks the server for a certificate, keeping private keys on the harness
// and sending only signing requests.
type enrolledCredentials struct {
	baseURL string
	token   string
}

func (c enrolledCredentials) forAgent(ctx context.Context, plan tenantAgent) (*tls.Config, error) {
	issued, err := EnrollAgent(ctx, EnrollOptions{
		BaseURL:         c.baseURL,
		EnrollmentToken: c.token,
		DeviceID:        uuid.New().String(),
		Hostname:        plan.hostname,
		// The machine's arrival and its later filing share one per-address allowance.
		PresentedAddress: presentedAddress(plan.agentIndex),
	})
	if err != nil {
		return nil, fmt.Errorf("enroll %s: %w", plan.hostname, err)
	}
	return issued.AgentTLSConfig()
}

// localCredentials signs against an authority this process owns, for a stack the run brought
// up itself.
type localCredentials struct {
	manager *cert.Manager
}

func (c localCredentials) forAgent(_ context.Context, plan tenantAgent) (*tls.Config, error) {
	deviceID := uuid.New()
	issued, err := c.manager.SignAgent(deviceID.String(), plan.hostname)
	if err != nil {
		return nil, fmt.Errorf("sign cert for %s: %w", plan.hostname, err)
	}
	return c.manager.AgentTLSConfig(issued), nil
}

// deviceIDFrom reads a machine's identifier from the common name of the certificate it dials
// with, which is where the server takes it from.
func deviceIDFrom(config *tls.Config) (string, bool) {
	if config == nil || len(config.Certificates) == 0 || len(config.Certificates[0].Certificate) == 0 {
		return "", false
	}
	leaf, err := x509.ParseCertificate(config.Certificates[0].Certificate[0])
	if err != nil || leaf.Subject.CommonName == "" {
		return "", false
	}
	return leaf.Subject.CommonName, true
}

// enrolOnce wraps a credential source so a machine's identity is minted once, keyed by its name,
// and reused on every reconnect.
func enrolOnce(source agentCredentials) agentCredentials {
	return &heldCredentials{source: source, held: map[string]*heldCredential{}}
}

// heldCredentials is the identities the run has minted so far.
type heldCredentials struct {
	source agentCredentials
	mu     sync.Mutex
	held   map[string]*heldCredential
}

// heldCredential is one machine's identity, and the guard that keeps a fleet
// starting together from minting it more than once.
type heldCredential struct {
	once   sync.Once
	config *tls.Config
	err    error
}

func (c *heldCredentials) forAgent(ctx context.Context, plan tenantAgent) (*tls.Config, error) {
	c.mu.Lock()
	entry, known := c.held[plan.hostname]
	if !known {
		entry = &heldCredential{}
		c.held[plan.hostname] = entry
	}
	c.mu.Unlock()

	entry.once.Do(func() { entry.config, entry.err = c.source.forAgent(ctx, plan) })

	if entry.err != nil {
		// A refusal is forgotten so the next start asks again.
		c.forget(plan.hostname, entry)
		return nil, entry.err
	}
	return entry.config, nil
}

// forget drops one machine's failed enrolment, unless a later start has already
// replaced it.
func (c *heldCredentials) forget(hostname string, entry *heldCredential) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.held[hostname] == entry {
		delete(c.held, hostname)
	}
}
