// Package devicecert issues machine certificates: at enrolment under a device number the server
// chooses, and at renewal for the number a connection proved, each recorded before it is sent.
package devicecert

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/certledger"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
)

// numberAttempts bounds the search for a fresh device number; a random one is taken at the first
// try in all but a vanishing share of cases.
const numberAttempts = 8

// ErrNoFreeNumber is a search that found only numbers already in use.
var ErrNoFreeNumber = errors.New("no free device number")

// ErrInvalidRequest is a certificate request that is malformed, unsigned, or for a key other
// than ECDSA P-256.
var ErrInvalidRequest = errors.New("invalid certificate request")

// tenantFinder finds the tenant holding a device number.
type tenantFinder interface {
	TenantForDevice(ctx context.Context, id device.DeviceID) (uuid.UUID, error)
}

// deletionChecker reports whether a device number was ever deleted.
type deletionChecker interface {
	IsDeviceNumberDeleted(ctx context.Context, deviceID uuid.UUID) (bool, error)
}

// Issuer signs device certificates and records each before handing it out.
type Issuer struct {
	cert    *cert.Manager
	ledger  *certledger.Store
	devices tenantFinder
	deleted deletionChecker
}

// NewIssuer returns an issuer over the trust set, the ledger, and the stores that say which
// device numbers are taken.
func NewIssuer(cm *cert.Manager, ledger *certledger.Store, devices tenantFinder, deleted deletionChecker) *Issuer {
	return &Issuer{cert: cm, ledger: ledger, devices: devices, deleted: deleted}
}

// CACertPEM is the trust bundle an enrolling machine is told to trust.
func (i *Issuer) CACertPEM() []byte {
	return i.cert.CACertPEM()
}

// Ledger is the store every issued certificate is recorded in.
func (i *Issuer) Ledger() *certledger.Store {
	return i.ledger
}

// Enroll issues a certificate for the request's key under a device number no device, deleted
// device or earlier certificate holds; the request's own subject is ignored.
func (i *Issuer) Enroll(ctx context.Context, csrDER []byte) (uuid.UUID, []byte, error) {
	csr, err := cert.ParseDeviceRequest(csrDER)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	id, err := i.freeNumber(ctx)
	if err != nil {
		return uuid.Nil, nil, err
	}
	der, err := i.Issue(ctx, csr, id, certledger.SourceEnroll)
	return id, der, err
}

// Issue signs a certificate naming deviceID for the request's key and records it; a record that
// fails returns an error and no certificate.
func (i *Issuer) Issue(ctx context.Context, csr *x509.CertificateRequest, deviceID uuid.UUID, source certledger.Source) ([]byte, error) {
	der, err := i.cert.IssueDevice(csr, deviceID)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse issued certificate: %w", err)
	}
	if _, err := i.ledger.Record(ctx, leaf, i.cert.IssuerHash(), source, i.cert.Now()); err != nil {
		return nil, err
	}
	return der, nil
}

func (i *Issuer) freeNumber(ctx context.Context) (uuid.UUID, error) {
	admin := dbtx.WithDefaultTenant(ctx, true)
	for range numberAttempts {
		id := uuid.New()
		if i.ledger.HasDevice(id) {
			continue
		}
		if _, err := i.devices.TenantForDevice(admin, id); err == nil {
			continue
		} else if !errors.Is(err, device.ErrDeviceNotFound) {
			return uuid.Nil, fmt.Errorf("check device number: %w", err)
		}
		deleted, err := i.deleted.IsDeviceNumberDeleted(ctx, id)
		if err != nil {
			return uuid.Nil, err
		}
		if !deleted {
			return id, nil
		}
	}
	return uuid.Nil, ErrNoFreeNumber
}
