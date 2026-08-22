package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/cert"
)

// Where a simulated machine's certificate comes from.
//
// There are two honest answers and they belong to different places. A local
// stack owns its own certificate authority, so the harness can sign against it
// directly. A shared environment does not: signing there means copying the
// authority's private key out of the cluster and onto a CI runner, and that key
// mints a trusted machine for the whole fleet. So against anything shared, the
// harness enrols the way an installer does — it keeps its private keys and
// sends only signing requests.
//
// Which one is in use is decided once, here, rather than at each call site.

// agentCredentials issues the TLS material one simulated machine dials with.
type agentCredentials interface {
	forAgent(ctx context.Context, plan tenantAgent) (*tls.Config, error)
}

// newAgentCredentials picks the source. An enrollment URL means the server
// signs; otherwise the harness signs against a local authority in dataDir.
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

// enrolledCredentials asks the server for a certificate, which is what keeps
// the authority's private key inside the cluster.
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
	})
	if err != nil {
		return nil, fmt.Errorf("enroll %s: %w", plan.hostname, err)
	}
	return issued.AgentTLSConfig()
}

// localCredentials signs against an authority this process owns. It is for a
// stack the run brought up itself, where the authority is as disposable as the
// stack around it.
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
