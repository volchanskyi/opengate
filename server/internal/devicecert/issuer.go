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
)

// numberAttempts bounds the search for a fresh device number; a random one is taken at the first
// try in all but a vanishing share of cases.
const numberAttempts = 8

// ErrNoFreeNumber is a search that found only numbers already in use.
var ErrNoFreeNumber = errors.New("no free device number")

// ErrInvalidRequest is a certificate request that is malformed, unsigned, or for a key other
// than ECDSA P-256.
var ErrInvalidRequest = errors.New("invalid certificate request")

// Issuer signs device certificates and records each before handing it out.
type Issuer struct {
	cert   *cert.Manager
	ledger *certledger.Store
}

// NewIssuer returns an issuer over the trust set and the ledger every certificate is recorded in.
func NewIssuer(cm *cert.Manager, ledger *certledger.Store) *Issuer {
	return &Issuer{cert: cm, ledger: ledger}
}

// CACertPEM is the trust bundle an enrolling machine is told to trust.
func (i *Issuer) CACertPEM() []byte {
	return i.cert.CACertPEM()
}

// Ledger is the store every issued certificate is recorded in.
func (i *Issuer) Ledger() *certledger.Store {
	return i.ledger
}

// Enroll issues a certificate for the request's key under a fresh device number nothing holds,
// ignoring the request's subject; a number found taken is signed again under another.
func (i *Issuer) Enroll(ctx context.Context, csrDER []byte) (uuid.UUID, []byte, error) {
	csr, err := cert.ParseDeviceRequest(csrDER)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	admin := dbtx.WithDefaultTenant(ctx, true)
	for range numberAttempts {
		id := uuid.New()
		if i.ledger.HasDevice(id) {
			continue
		}
		der, leaf, err := i.sign(csr, id)
		if err != nil {
			return uuid.Nil, nil, err
		}
		_, claimed, err := i.ledger.Claim(admin, leaf, i.cert.IssuerHash(), i.cert.Now())
		if err != nil {
			return uuid.Nil, nil, fmt.Errorf("check device number: %w", err)
		}
		if claimed {
			return id, der, nil
		}
	}
	return uuid.Nil, nil, ErrNoFreeNumber
}

// Issue signs a certificate naming deviceID for the request's key and records it; a record that
// fails returns an error and no certificate.
func (i *Issuer) Issue(ctx context.Context, csr *x509.CertificateRequest, deviceID uuid.UUID, source certledger.Source) ([]byte, error) {
	der, leaf, err := i.sign(csr, deviceID)
	if err != nil {
		return nil, err
	}
	if _, err := i.ledger.Record(ctx, leaf, i.cert.IssuerHash(), source, i.cert.Now()); err != nil {
		return nil, err
	}
	return der, nil
}

// sign issues a certificate naming deviceID for the request's key, with its parsed leaf.
func (i *Issuer) sign(csr *x509.CertificateRequest, deviceID uuid.UUID) ([]byte, *x509.Certificate, error) {
	der, err := i.cert.IssueDevice(csr, deviceID)
	if err != nil {
		return nil, nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("parse issued certificate: %w", err)
	}
	return der, leaf, nil
}
