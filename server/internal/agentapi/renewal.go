package agentapi

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/certledger"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/devicecert"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

const (
	// renewalAnswerWait is how long an offer waits for the machine's request.
	renewalAnswerWait = 60 * time.Second
	// maxCSRBytes bounds a renewal request.
	maxCSRBytes = 4 * 1024
	// renewalsPerDay bounds issuances per device in a rolling day, across restarts.
	renewalsPerDay = 3
	renewalWindow  = 24 * time.Hour
)

// The reasons a renewal is offered or refused, as the machine receives them.
const (
	reasonDue             = "due"
	reasonAuthorityChange = "authority-change"
	refusedUnsolicited    = "unsolicited"
	refusedInvalid        = "invalid-request"
	refusedWrongDevice    = "wrong-device"
	refusedSuperseded     = "superseded"
	refusedKeyType        = "unsupported-key"
	refusedKeyReuse       = "key-reuse"
	refusedDeleted        = "deleted"
	refusedUnknownDevice  = "unknown-device"
	refusedRateLimited    = "rate-limited"
	refusedInFlight       = "in-flight"
	refusedServerError    = "server-error"
)

// renewals is the server's side of certificate renewal: issuance, the per-device in-flight
// guard, and closing what a renewal supersedes.
type renewals struct {
	cert     *cert.Manager
	issuer   *devicecert.Issuer
	devices  device.Repository
	deleted  func(uuid.UUID) bool
	inFlight sync.Map // map[uuid.UUID]struct{}
	// closeSuperseded closes every live connection presenting one of the given certificates.
	closeSuperseded func([]certledger.Fingerprint)
}

// renewalState is one connection's offer: idle until asked, then waiting for the request.
type renewalState struct {
	mu      sync.Mutex
	askedAt time.Time
	// issued is the certificate issued on this connection, made current by its install notice.
	issued    *certledger.Fingerprint
	issuedFor *x509.Certificate
}

// renewalReason says whether the certificate is due, at two thirds of its lifetime or under a
// retiring master key, and why; a renewal the issuer would cap at the current end is skipped.
func (a *AgentConn) renewalReason(now time.Time) (string, bool) {
	if a.renewal == nil || a.leaf == nil {
		return "", false
	}
	if a.authority.Role != cert.RoleIssuer {
		return reasonAuthorityChange, true
	}
	lifetime := a.leaf.NotAfter.Sub(a.leaf.NotBefore)
	if now.Before(a.leaf.NotBefore.Add(lifetime * 2 / 3)) {
		return "", false
	}
	if !a.renewal.cert.NextLeafEnd().After(a.leaf.NotAfter) {
		return "", false
	}
	return reasonDue, true
}

// offerRenewal asks the machine to renew when it is due and no offer is outstanding.
func (a *AgentConn) offerRenewal(ctx context.Context) {
	if a.renewal == nil {
		return
	}
	now := a.renewal.cert.Now()
	reason, due := a.renewalReason(now)
	if !due {
		return
	}
	a.renewalState.mu.Lock()
	if !a.renewalState.askedAt.IsZero() && now.Sub(a.renewalState.askedAt) <= renewalAnswerWait {
		a.renewalState.mu.Unlock()
		return
	}
	a.renewalState.askedAt = now
	a.renewalState.mu.Unlock()
	if err := a.SendRenewCertificate(ctx, reason); err != nil {
		a.logger.Warn("offer certificate renewal", "device_id", a.DeviceID, "error", err)
	}
}

// takeOffer consumes an outstanding offer no older than renewalAnswerWait.
func (a *AgentConn) takeOffer(now time.Time) bool {
	a.renewalState.mu.Lock()
	defer a.renewalState.mu.Unlock()
	asked := a.renewalState.askedAt
	a.renewalState.askedAt = time.Time{}
	return !asked.IsZero() && now.Sub(asked) <= renewalAnswerWait
}

// handleCertificateRequest answers a renewal request: refused with a reason, or issued, recorded
// and sent with the trust bundle.
func (a *AgentConn) handleCertificateRequest(ctx context.Context, msg *protocol.ControlMessage) error {
	if a.renewal == nil {
		return nil
	}
	issued, leaf, reason, retryAfter := a.renewFor(ctx, msg.CSR)
	if reason != "" {
		a.logger.Warn("certificate renewal refused", "device_id", a.DeviceID, "reason", reason)
		return a.SendCertificateRefused(ctx, reason, retryAfter)
	}
	fp := certledger.FingerprintOf(issued)
	a.renewalState.mu.Lock()
	a.renewalState.issued = &fp
	a.renewalState.issuedFor = leaf
	a.renewalState.mu.Unlock()
	return a.SendCertificateIssued(ctx, issued, string(a.renewal.cert.CACertPEM()))
}

// renewFor runs the renewal checks in order and issues; a refusal returns its reason.
func (a *AgentConn) renewFor(ctx context.Context, csrDER []byte) ([]byte, *x509.Certificate, string, uint32) {
	r := a.renewal
	now := r.cert.Now()
	if !a.takeOffer(now) {
		return nil, nil, refusedUnsolicited, 0
	}
	if len(csrDER) > maxCSRBytes {
		return nil, nil, refusedInvalid, 0
	}
	csr, err := cert.ParseDeviceRequest(csrDER)
	if errors.Is(err, cert.ErrNotP256) {
		return nil, nil, refusedKeyType, 0
	}
	if err != nil {
		return nil, nil, refusedInvalid, 0
	}
	if reason := a.requestMismatch(csr); reason != "" {
		return nil, nil, reason, 0
	}
	if reason, retry := a.deviceRefusal(ctx, now); reason != "" {
		return nil, nil, reason, retry
	}
	if _, busy := r.inFlight.LoadOrStore(a.DeviceID, struct{}{}); busy {
		return nil, nil, refusedInFlight, 0
	}
	defer r.inFlight.Delete(a.DeviceID)
	der, err := r.issuer.Issue(ctx, csr, a.DeviceID, certledger.SourceRenew)
	if err != nil {
		a.logger.Error("issue renewed certificate", "device_id", a.DeviceID, "error", err)
		return nil, nil, refusedServerError, 0
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, refusedServerError, 0
	}
	return der, leaf, "", 0
}

// requestMismatch checks the request names this connection's device, comes from its current
// certificate, and asks for a new key.
func (a *AgentConn) requestMismatch(csr *x509.CertificateRequest) string {
	if id, err := uuid.Parse(csr.Subject.CommonName); err != nil || id != a.DeviceID {
		return refusedWrongDevice
	}
	if a.renewal.issuer.Ledger().Superseded(a.certFP) {
		return refusedSuperseded
	}
	if bytes.Equal(csr.RawSubjectPublicKeyInfo, a.leaf.RawSubjectPublicKeyInfo) {
		return refusedKeyReuse
	}
	return ""
}

// deviceRefusal checks the device is neither deleted nor unknown and within its daily allowance.
func (a *AgentConn) deviceRefusal(ctx context.Context, now time.Time) (string, uint32) {
	r := a.renewal
	if r.deleted != nil && r.deleted(a.DeviceID) {
		return refusedDeleted, 0
	}
	if r.devices != nil {
		if _, err := r.devices.TenantForDevice(dbtx.WithDefaultTenant(ctx, true), a.DeviceID); err != nil {
			return refusedUnknownDevice, 0
		}
	}
	since := now.Add(-renewalWindow)
	ledger := r.issuer.Ledger()
	count, err := ledger.RenewalsSince(ctx, a.DeviceID, since)
	if err != nil {
		return refusedServerError, 0
	}
	if count < renewalsPerDay {
		return "", 0
	}
	oldest, err := ledger.OldestRenewalSince(ctx, a.DeviceID, since)
	if err != nil {
		return refusedServerError, 0
	}
	wait := oldest.Add(renewalWindow).Sub(now)
	return refusedRateLimited, uint32(max(1, min(wait, renewalWindow)/time.Second))
}

// handleCertificateInstalled makes the certificate issued on this connection current, which
// supersedes the older ones and closes every other connection still presenting one.
func (a *AgentConn) handleCertificateInstalled(ctx context.Context) error {
	if a.renewal == nil {
		return nil
	}
	a.renewalState.mu.Lock()
	issued, leaf := a.renewalState.issued, a.renewalState.issuedFor
	a.renewalState.issued, a.renewalState.issuedFor = nil, nil
	a.renewalState.mu.Unlock()
	if issued == nil {
		return nil
	}
	superseded, err := a.renewal.issuer.Ledger().MarkInstalled(ctx, *issued, a.renewal.cert.Now())
	if err != nil {
		a.logger.Error("record installed certificate", "device_id", a.DeviceID, "error", err)
	}
	a.certFP, a.leaf = *issued, leaf
	a.authority = a.renewal.authorityOf(leaf)
	a.logger.Info("certificate renewed", "device_id", a.DeviceID, "not_after", leaf.NotAfter)
	if len(superseded) > 0 && a.renewal.closeSuperseded != nil {
		a.renewal.closeSuperseded(superseded)
	}
	return nil
}

// authorityOf names the trusted master key that signed a freshly issued certificate.
func (r *renewals) authorityOf(leaf *x509.Certificate) cert.Authority {
	for _, a := range r.cert.Authorities() {
		if leaf.CheckSignatureFrom(a.Cert) == nil {
			return a
		}
	}
	return cert.Authority{}
}

// SendRenewCertificate asks the machine for a certificate request under a new key.
func (a *AgentConn) SendRenewCertificate(ctx context.Context, reason string) error {
	return a.sendControl(&protocol.ControlMessage{Type: protocol.MsgRenewCertificate, Reason: reason})
}

// SendCertificateIssued sends a renewed certificate and the trust bundle that verifies the server.
func (a *AgentConn) SendCertificateIssued(ctx context.Context, certDER []byte, bundle string) error {
	return a.sendControl(&protocol.ControlMessage{
		Type:     protocol.MsgCertificateIssued,
		Cert:     certDER,
		CABundle: bundle,
	})
}

// SendCertificateRefused refuses a renewal request, with seconds to wait before the next.
func (a *AgentConn) SendCertificateRefused(ctx context.Context, reason string, retryAfter uint32) error {
	return a.sendControl(&protocol.ControlMessage{
		Type:       protocol.MsgCertificateRefused,
		Reason:     reason,
		RetryAfter: retryAfter,
	})
}

// SendTrustBundle sends the master keys the machine trusts; its arrival tells the machine the
// server accepted its identity.
func (a *AgentConn) SendTrustBundle(ctx context.Context, bundle string) error {
	return a.sendControl(&protocol.ControlMessage{Type: protocol.MsgTrustBundle, CABundle: bundle})
}
