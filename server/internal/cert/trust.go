package cert

import (
	"bytes"
	"crypto"
	"crypto/sha512"
	"crypto/x509"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// Role is what a trusted master key is for, read from the file that holds it.
type Role int

const (
	// RoleIssuer signs new certificates and is fully accepted: ca.crt and ca.key.
	RoleIssuer Role = iota
	// RoleAccepted is fully accepted and signs nothing: ca-accepted.crt.
	RoleAccepted
	// RoleRenewOnly admits a machine only to renew: ca-renew-only.crt.
	RoleRenewOnly
	// RoleVouchedOnly admits a machine to renew only with a ledger entry from before the
	// compromise: ca-vouched-only.crt.
	RoleVouchedOnly
)

// String names the role as the authority tool prints it.
func (r Role) String() string {
	switch r {
	case RoleIssuer:
		return "issuer"
	case RoleAccepted:
		return "accepted"
	case RoleRenewOnly:
		return "renew-only"
	case RoleVouchedOnly:
		return "vouched-only"
	default:
		return "unknown"
	}
}

// The files a trust set is read from.
const (
	IssuerCertFile      = "ca.crt"
	IssuerKeyFile       = "ca.key"
	AcceptedFile        = "ca-accepted.crt"
	RenewOnlyFile       = "ca-renew-only.crt"
	VouchedOnlyFile     = "ca-vouched-only.crt"
	ChainFile           = "ca-chain.crt"
	MPSIssuerCertFile   = "mps-ca.crt"
	MPSIssuerKeyFile    = "mps-ca.key"
	keyFileForbiddenBit = 0o077
)

// Authority is one trusted master key.
type Authority struct {
	Cert *x509.Certificate
	Role Role
	// Hash is the SHA-384 of the certificate, the fingerprint the fast path caches.
	Hash [48]byte
}

// TrustSet is the master keys a server trusts, read from its role files.
type TrustSet struct {
	issuer       *x509.Certificate
	issuerKey    crypto.Signer
	accepted     []*x509.Certificate
	renewOnly    []*x509.Certificate
	vouchedOnly  []*x509.Certificate
	chain        [][]byte
	mpsIssuer    *x509.Certificate
	mpsIssuerKey crypto.Signer
}

func (s *TrustSet) authorities() []Authority {
	out := []Authority{authority(s.issuer, RoleIssuer)}
	for _, group := range []struct {
		certs []*x509.Certificate
		role  Role
	}{{s.accepted, RoleAccepted}, {s.renewOnly, RoleRenewOnly}, {s.vouchedOnly, RoleVouchedOnly}} {
		for _, c := range group.certs {
			out = append(out, authority(c, group.role))
		}
	}
	return out
}

func authority(c *x509.Certificate, role Role) Authority {
	return Authority{Cert: c, Role: role, Hash: sha512.Sum384(c.Raw)}
}

// LoadTrustSet reads the role files in dir, generating a missing issuer unless mustExist and
// refusing an inconsistent set.
func LoadTrustSet(dir string, mustExist bool, now func() time.Time) (*TrustSet, error) {
	issuerCert := filepath.Join(dir, IssuerCertFile)
	issuerKey := filepath.Join(dir, IssuerKeyFile)
	if !fileExists(issuerCert) || !fileExists(issuerKey) {
		if mustExist {
			return nil, fmt.Errorf("the issuing master key is missing from %s and must exist", dir)
		}
		if err := generateAuthorityFiles(dir, "OpenGate CA", now()); err != nil {
			return nil, err
		}
	}

	set := &TrustSet{}
	var err error
	if set.issuer, set.issuerKey, err = loadKeyPair(issuerCert, issuerKey); err != nil {
		return nil, err
	}
	set.mpsIssuer, set.mpsIssuerKey = set.issuer, set.issuerKey
	if fileExists(filepath.Join(dir, MPSIssuerCertFile)) {
		if set.mpsIssuer, set.mpsIssuerKey, err = loadKeyPair(
			filepath.Join(dir, MPSIssuerCertFile), filepath.Join(dir, MPSIssuerKeyFile)); err != nil {
			return nil, err
		}
	}
	for _, f := range []struct {
		name string
		into *[]*x509.Certificate
	}{{AcceptedFile, &set.accepted}, {RenewOnlyFile, &set.renewOnly}, {VouchedOnlyFile, &set.vouchedOnly}} {
		if *f.into, err = loadOptionalCerts(filepath.Join(dir, f.name)); err != nil {
			return nil, err
		}
	}
	chain, err := loadOptionalCerts(filepath.Join(dir, ChainFile))
	if err != nil {
		return nil, err
	}
	for _, c := range chain {
		set.chain = append(set.chain, c.Raw)
	}
	return set, set.validate(chain)
}

// validate refuses a certificate in two roles, and accepts a chain entry only as the issuer
// cross-signed by a trusted master key.
func (s *TrustSet) validate(chain []*x509.Certificate) error {
	seen := map[[48]byte]Role{}
	for _, a := range s.authorities() {
		if role, dup := seen[a.Hash]; dup {
			return fmt.Errorf("master key %x holds two roles: %s and %s", a.Hash[:6], role, a.Role)
		}
		seen[a.Hash] = a.Role
	}
	for _, c := range chain {
		if !bytes.Equal(c.RawSubject, s.issuer.RawSubject) ||
			!bytes.Equal(c.RawSubjectPublicKeyInfo, s.issuer.RawSubjectPublicKeyInfo) {
			return errors.New("a chain entry is not the issuing master key cross-signed")
		}
		if !s.signedByTrusted(c) {
			return errors.New("a chain entry does not verify against any trusted master key")
		}
	}
	return nil
}

func (s *TrustSet) signedByTrusted(c *x509.Certificate) bool {
	for _, a := range s.authorities() {
		if a.Role != RoleIssuer && c.CheckSignatureFrom(a.Cert) == nil {
			return true
		}
	}
	return false
}
