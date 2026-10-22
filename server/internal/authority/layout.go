// Package authority lays out the master keys' role files for each phase of replacing the master
// key, and stages them for the server; it prints roles and fingerprints, never key material.
package authority

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/volchanskyi/opengate/server/internal/cert"
)

// The staging files a kit and a changeover keep beside the role files, which the server ignores.
const (
	NextCertFile     = "next-ca.crt"
	NextKeyFile      = "next-ca.key"
	NextCrossFile    = "next-cross.crt"
	PreviousCertFile = "previous-ca.crt"
	PreviousKeyFile  = "previous-ca.key"
)

// Files is a trust set's files by name.
type Files map[string][]byte

// Names lists the files present, sorted.
func (f Files) Names() []string {
	names := make([]string, 0, len(f))
	for name, data := range f {
		if len(data) > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Phase is a step of replacing the master key; A is the key that issues now, B the next.
type Phase string

const (
	// PhaseSteady keeps only the issuer.
	PhaseSteady Phase = "steady"
	// PhaseChangeover issues from B and still fully accepts A.
	PhaseChangeover Phase = "changeover"
	// PhaseRetire issues from B and admits A only to renew.
	PhaseRetire Phase = "retire"
	// PhaseEmergency issues from B and admits A only with a voucher from before its leak.
	PhaseEmergency Phase = "emergency"
	// PhaseSettle issues from B and keeps A renew-only until its last certificate lapses.
	PhaseSettle Phase = "settle"
)

// MPSChoice names the master key Intel AMT's MPS certificate is signed with.
type MPSChoice string

const (
	// MPSOld keeps AMT on the key its firmware was provisioned with.
	MPSOld MPSChoice = ""
	// MPSNew moves AMT to the issuer once the firmware is reprovisioned, which destroys A.
	MPSNew MPSChoice = "new"
)

// LayoutOptions are the operator's choices within a phase.
type LayoutOptions struct {
	MPS MPSChoice
	// KeepChain keeps the cross certificate after settling.
	KeepChain bool
	// DropOld drops A entirely when settling.
	DropOld bool
}

var (
	// ErrKeyExists refuses to create a master key over one that exists.
	ErrKeyExists = errors.New("a master key already exists")
	// ErrKitExists refuses a second kit while one is staged.
	ErrKitExists = errors.New("a next master key is already staged")
	// ErrNoKit is a phase that needs a staged next key, and none is.
	ErrNoKit = errors.New("no next master key is staged: run new-kit first")
	// ErrNotInChangeover is a phase that follows a changeover, and none happened.
	ErrNotInChangeover = errors.New("the trust set is not in a changeover")
	// ErrNoIssuer is a trust set without its issuing master key.
	ErrNoIssuer = errors.New("the trust set has no issuing master key")
)

const (
	issuerName = "OpenGate CA"
	nextName   = "OpenGate CA (next)"
)

// Init creates the issuing master key for a new cluster.
func Init(current Files, now time.Time) (Files, error) {
	if len(current[cert.IssuerKeyFile]) > 0 {
		return nil, ErrKeyExists
	}
	certPEM, keyPEM, err := cert.NewAuthority(issuerName, now)
	if err != nil {
		return nil, err
	}
	return Files{cert.IssuerCertFile: certPEM, cert.IssuerKeyFile: keyPEM}, nil
}

// NewKit stages the next master key and its certificate signed by the current one, leaving
// every role as it is.
func NewKit(current Files, now time.Time) (Files, error) {
	if len(current[cert.IssuerKeyFile]) == 0 {
		return nil, ErrNoIssuer
	}
	if len(current[NextKeyFile]) > 0 {
		return nil, ErrKitExists
	}
	issuer, err := cert.ParseCertificatePEM(current[cert.IssuerCertFile])
	if err != nil {
		return nil, fmt.Errorf("issuer: %w", err)
	}
	issuerKey, err := cert.ParseKeyPEM(current[cert.IssuerKeyFile])
	if err != nil {
		return nil, fmt.Errorf("issuer key: %w", err)
	}
	nextPEM, nextKeyPEM, err := cert.NewAuthority(nextName, now)
	if err != nil {
		return nil, err
	}
	next, err := cert.ParseCertificatePEM(nextPEM)
	if err != nil {
		return nil, err
	}
	cross, err := cert.CrossSign(next, issuer, issuerKey)
	if err != nil {
		return nil, err
	}
	out := copyFiles(current)
	out[NextCertFile], out[NextKeyFile], out[NextCrossFile] = nextPEM, nextKeyPEM, cross
	return out, nil
}

// Layout writes the role files for a phase from the files of the phase before it.
func Layout(current Files, phase Phase, opts LayoutOptions) (Files, error) {
	if len(current[cert.IssuerKeyFile]) == 0 {
		return nil, ErrNoIssuer
	}
	switch phase {
	case PhaseSteady:
		return Files{cert.IssuerCertFile: current[cert.IssuerCertFile], cert.IssuerKeyFile: current[cert.IssuerKeyFile]}, nil
	case PhaseChangeover, PhaseEmergency:
		return fromKit(current, phase, opts)
	case PhaseRetire, PhaseSettle:
		return afterChangeover(current, phase, opts)
	default:
		return nil, fmt.Errorf("unknown phase %q", phase)
	}
}

// fromKit promotes the staged key to issuer, with A accepted or vouched-only beside it.
func fromKit(current Files, phase Phase, opts LayoutOptions) (Files, error) {
	if len(current[NextKeyFile]) == 0 {
		return nil, ErrNoKit
	}
	old, oldKey := current[cert.IssuerCertFile], current[cert.IssuerKeyFile]
	out := Files{
		cert.IssuerCertFile: current[NextCertFile],
		cert.IssuerKeyFile:  current[NextKeyFile],
		cert.ChainFile:      current[NextCrossFile],
		PreviousCertFile:    old,
	}
	if phase == PhaseChangeover {
		out[cert.AcceptedFile] = old
	} else {
		out[cert.VouchedOnlyFile] = old
	}
	if opts.MPS != MPSNew {
		out[cert.MPSIssuerCertFile], out[cert.MPSIssuerKeyFile], out[PreviousKeyFile] = old, oldKey, oldKey
	}
	return out, nil
}

// afterChangeover moves A to renew-only, or out, keeping the chain while A is still trusted.
func afterChangeover(current Files, phase Phase, opts LayoutOptions) (Files, error) {
	old := current[PreviousCertFile]
	if len(old) == 0 {
		return nil, ErrNotInChangeover
	}
	out := Files{
		cert.IssuerCertFile: current[cert.IssuerCertFile],
		cert.IssuerKeyFile:  current[cert.IssuerKeyFile],
	}
	keepOld := phase == PhaseRetire || !opts.DropOld
	if keepOld {
		out[cert.RenewOnlyFile] = old
		out[PreviousCertFile] = old
	}
	if phase == PhaseRetire || (opts.KeepChain && keepOld) {
		out[cert.ChainFile] = current[cert.ChainFile]
	}
	oldKey := current[PreviousKeyFile]
	if phase == PhaseRetire && opts.MPS != MPSNew && len(oldKey) > 0 {
		out[cert.MPSIssuerCertFile], out[cert.MPSIssuerKeyFile], out[PreviousKeyFile] = old, oldKey, oldKey
	}
	return out, nil
}

func copyFiles(f Files) Files {
	out := Files{}
	for k, v := range f {
		out[k] = v
	}
	return out
}
