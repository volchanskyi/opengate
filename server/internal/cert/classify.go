package cert

import (
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Standing is how far a presented certificate admits its machine.
type Standing int

const (
	// StandingFull admits the machine to everything.
	StandingFull Standing = iota
	// StandingRenewOnly admits the machine only to renew or to receive its removal order.
	StandingRenewOnly
	// StandingNeedsVoucher admits the machine to renew only with a ledger entry from before the
	// compromise of the master key that signed it.
	StandingNeedsVoucher
	// StandingTooOld is a certificate expired past the grace; the machine is refused unless it is
	// being told to remove itself.
	StandingTooOld
)

// String names the standing for logs.
func (s Standing) String() string {
	switch s {
	case StandingFull:
		return "full"
	case StandingRenewOnly:
		return "renew-only"
	case StandingNeedsVoucher:
		return "needs-voucher"
	case StandingTooOld:
		return "too-old"
	default:
		return "unknown"
	}
}

// ErrUnacceptable is a presented certificate that fails TLS: missing, malformed, not yet valid,
// or not signed for client use by a trusted master key.
var ErrUnacceptable = errors.New("client certificate refused")

// Classification is what Classify learned about a presented certificate.
type Classification struct {
	DeviceID  uuid.UUID
	Leaf      *x509.Certificate
	Authority Authority
	Standing  Standing
	// Expired is set once the leaf or its master key is past its expiry.
	Expired bool
}

// Classify verifies a presented chain against every trusted master key at the leaf's issuance
// time, so an expired one still verifies, then reads its standing from the signer's role.
func (m *Manager) Classify(peer []*x509.Certificate) (Classification, error) {
	if len(peer) == 0 {
		return Classification{}, fmt.Errorf("%w: none presented", ErrUnacceptable)
	}
	leaf := peer[0]
	deviceID, err := uuid.Parse(leaf.Subject.CommonName)
	if err != nil {
		return Classification{}, fmt.Errorf("%w: common name %q is no device number", ErrUnacceptable, leaf.Subject.CommonName)
	}
	now := m.now()
	if now.Before(leaf.NotBefore) {
		return Classification{}, fmt.Errorf("%w: not yet valid", ErrUnacceptable)
	}
	signer, err := m.verifyAtIssuance(leaf)
	if err != nil {
		return Classification{}, err
	}

	expiry := leaf.NotAfter
	if signer.Cert.NotAfter.Before(expiry) {
		expiry = signer.Cert.NotAfter
	}
	c := Classification{DeviceID: deviceID, Leaf: leaf, Authority: signer, Expired: now.After(expiry)}
	switch {
	case c.Expired && now.Sub(expiry) > m.expiredGrace:
		c.Standing = StandingTooOld
	case signer.Role == RoleVouchedOnly:
		c.Standing = StandingNeedsVoucher
	case c.Expired || signer.Role == RoleRenewOnly:
		c.Standing = StandingRenewOnly
	default:
		c.Standing = StandingFull
	}
	return c, nil
}

// verifyAtIssuance verifies the leaf alone, without intermediates the client supplied, at a time
// inside its own validity, and returns the trusted master key that signed it.
func (m *Manager) verifyAtIssuance(leaf *x509.Certificate) (Authority, error) {
	at := leaf.NotBefore.Add(clockSkewMargin)
	if at.After(leaf.NotAfter) {
		at = leaf.NotAfter
	}
	chains, err := leaf.Verify(x509.VerifyOptions{
		Roots:       m.pool(),
		CurrentTime: at,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil || len(chains) == 0 || len(chains[0]) < 2 {
		return Authority{}, fmt.Errorf("%w: %v", ErrUnacceptable, err)
	}
	root := chains[0][len(chains[0])-1]
	for _, a := range m.authorities {
		if a.Cert.Equal(root) {
			return a, nil
		}
	}
	return Authority{}, fmt.Errorf("%w: signed by no trusted master key", ErrUnacceptable)
}

// ExpiredFor reports how long past its expiry a classified certificate is.
func (c Classification) ExpiredFor(now time.Time) time.Duration {
	expiry := c.Leaf.NotAfter
	if c.Authority.Cert.NotAfter.Before(expiry) {
		expiry = c.Authority.Cert.NotAfter
	}
	return now.Sub(expiry)
}
