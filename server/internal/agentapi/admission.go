package agentapi

import (
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"

	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/certledger"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// removalOrderWait bounds how long a deleted machine has to read its removal order and hang up.
const removalOrderWait = 10 * time.Second

// refusalLogPeriod de-duplicates the refusal log per device or certificate and reason.
const refusalLogPeriod = time.Hour

// hourlyLogKeys is how many keys the de-duplication holds before it drops lapsed ones.
const hourlyLogKeys = 4096

// admission is what a connection that passed TLS may do.
type admission struct {
	class cert.Classification
	fp    certledger.Fingerprint
	// refusal names why the connection is refused; empty admits it.
	refusal string
}

// restricted reports an admission only to renew or receive a removal order.
func (a admission) restricted() bool {
	return a.class.Standing != cert.StandingFull
}

// admit classifies the presented certificate and checks it against the ledger. A deleted device is
// recognised by the caller first, so its removal order reaches it whatever its certificate's age.
func (s *AgentServer) admit(peer []*x509.Certificate) admission {
	class, err := s.cert.Classify(peer)
	if err != nil {
		return admission{refusal: "unacceptable"}
	}
	a := admission{class: class, fp: certledger.FingerprintOf(class.Leaf.Raw)}
	if class.Standing == cert.StandingTooOld {
		a.refusal = "too-old"
		return a
	}
	if s.issuer == nil {
		a.refusal = "no-ledger"
		return a
	}
	ledger := s.issuer.Ledger()
	entry, recorded := ledger.Lookup(a.fp)
	switch {
	case !recorded:
		a.refusal = "not-on-ledger"
	case ledger.Superseded(a.fp):
		a.refusal = "superseded"
	case class.Standing == cert.StandingNeedsVoucher && !entry.IssuedAt.Before(s.cert.CompromisedSince()):
		a.refusal = "unvouched"
	}
	return a
}

// refuse logs a refused certificate, once an hour per device and reason, and closes with code 5.
func (s *AgentServer) refuse(conn *quic.Conn, a admission, logger *slog.Logger) {
	key := fmt.Sprintf("%x/%s", a.fp[:8], a.refusal)
	if a.class.DeviceID != uuid.Nil {
		key = a.class.DeviceID.String() + "/" + a.refusal
	}
	if s.refusals.due(key, time.Now()) {
		args := []any{"reason", a.refusal, "fingerprint", fmt.Sprintf("%x", a.fp[:6])}
		if a.class.Leaf != nil {
			args = append(args,
				"device_id", a.class.DeviceID,
				"issuer", fmt.Sprintf("%x", a.class.Authority.Hash[:6]),
				"not_after", a.class.Leaf.NotAfter)
		}
		logger.Warn("client certificate refused", args...)
	}
	closeWithCode(conn, protocol.CloseCertificateRefused, "certificate refused")
}

// deliverRemovalOrder writes the removal order, closes the server's sending side, and waits for
// the machine to hang up before closing with code 3, so the order is read and never discarded.
func (s *AgentServer) deliverRemovalOrder(stream *quic.Stream, conn *quic.Conn, logger *slog.Logger) {
	logger.Info("rejecting tombstoned device")
	codec := &protocol.Codec{}
	msg := &protocol.ControlMessage{
		Type:   protocol.MsgAgentDeregistered,
		Reason: "device deleted by administrator",
	}
	if payload, err := codec.EncodeControl(msg); err != nil {
		logger.Warn("encode tombstone deregister", "error", err)
	} else if err := codec.WriteFrame(stream, protocol.FrameControl, payload); err != nil {
		logger.Warn("write tombstone deregister frame", "error", err)
	}
	_ = stream.Close()

	hungUp := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, stream)
		close(hungUp)
	}()
	timer := time.NewTimer(removalOrderWait)
	defer timer.Stop()
	select {
	case <-conn.Context().Done():
	case <-hungUp:
	case <-timer.C:
		logger.Info("deleted machine did not hang up after its removal order")
	}
	closeWithCode(conn, protocol.CloseDeviceDeleted, "device deregistered")
}

func closeWithCode(conn *quic.Conn, code uint64, reason string) {
	_ = conn.CloseWithError(quic.ApplicationErrorCode(code), reason)
}

// hourlyLog lets a keyed log line through once per period.
type hourlyLog struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (h *hourlyLog) due(key string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.last == nil {
		h.last = map[string]time.Time{}
	}
	if len(h.last) >= hourlyLogKeys {
		for k, at := range h.last {
			if now.Sub(at) >= refusalLogPeriod {
				delete(h.last, k)
			}
		}
	}
	if at, ok := h.last[key]; ok && now.Sub(at) < refusalLogPeriod {
		return false
	}
	h.last[key] = now
	return true
}
