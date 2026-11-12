package agentapi

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// resendSpread spreads one round of re-send requests over a minute of catch-up requests.
const resendSpread = 60 * time.Second

// resendState remembers the last store restart while the server runs, and which machines
// were asked after it, so a machine offline then is asked once at its next connect.
type resendState struct {
	mu    sync.Mutex
	since time.Time
	told  map[uuid.UUID]struct{}
}

// resendTarget is one machine to ask and where it sends again from.
type resendTarget struct {
	conn  *AgentConn
	since time.Time
}

// SendResendRequest asks the machine to forget what it was told is stored after since and send
// it again after retryAfter. Requires the ResendRequests capability.
func (a *AgentConn) SendResendRequest(_ context.Context, since time.Time, retryAfter time.Duration) error {
	if err := a.requireCapability(protocol.CapResendRequests); err != nil {
		return err
	}
	return a.sendControl(&protocol.ControlMessage{
		Type:       protocol.MsgDeferBackfill,
		RetryAfter: clampNonNegativeUint32(int(max(retryAfter, minRetryAfter) / time.Second)),
		SinceTS:    since.Unix(),
	})
}

// RequestResendAfterRestart asks every connected machine that reads re-send requests to send
// again everything after since, and remembers since for the machines that connect later.
func (s *AgentServer) RequestResendAfterRestart(since time.Time) {
	s.resend.mu.Lock()
	s.resend.since = since
	s.resend.told = map[uuid.UUID]struct{}{}
	s.resend.mu.Unlock()
	if s.metrics != nil {
		s.metrics.ObserveStoreRestart()
	}
	s.logger.Warn("metrics store restarted; asking machines to send their history again", "since", since)

	var targets []resendTarget
	for _, ac := range s.ListConnectedAgents() {
		if ac.requireCapability(protocol.CapResendRequests) == nil {
			targets = append(targets, resendTarget{conn: ac, since: since})
		}
	}
	asked := s.ask(targets)
	s.resend.mu.Lock()
	for _, id := range asked {
		s.resend.told[id] = struct{}{}
	}
	s.resend.mu.Unlock()
	if s.metrics != nil {
		s.metrics.ObserveResendRequests(appmetrics.ResendStoreRestart, len(asked))
	}
}

// resendOnConnect asks a machine that registered after a store restart, once per restart.
func (s *AgentServer) resendOnConnect(ac *AgentConn) {
	if ac.requireCapability(protocol.CapResendRequests) != nil {
		return
	}
	s.resend.mu.Lock()
	since := s.resend.since
	_, told := s.resend.told[ac.DeviceID]
	if since.IsZero() || told {
		s.resend.mu.Unlock()
		return
	}
	s.resend.told[ac.DeviceID] = struct{}{}
	s.resend.mu.Unlock()
	if len(s.ask([]resendTarget{{conn: ac, since: since}})) > 0 && s.metrics != nil {
		s.metrics.ObserveResendRequests(appmetrics.ResendStoreRestart, 1)
	}
}

// RunLiveDropResends polls the writer's dropped live minutes every interval and asks those
// machines alone to send again, until ctx ends.
func (s *AgentServer) RunLiveDropResends(ctx context.Context, drops func() map[uuid.UUID]time.Time, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if dropped := drops(); len(dropped) > 0 {
				s.resendLiveDrops(dropped)
			}
		}
	}
}

// resendLiveDrops asks each connected machine that reads re-send requests to send again from
// its first dropped live minute; a machine gone since catches up when it returns.
func (s *AgentServer) resendLiveDrops(dropped map[uuid.UUID]time.Time) {
	var targets []resendTarget
	for id, since := range dropped {
		ac := s.GetAgent(id)
		if ac == nil || ac.requireCapability(protocol.CapResendRequests) != nil {
			continue
		}
		targets = append(targets, resendTarget{conn: ac, since: since})
	}
	asked := s.ask(targets)
	if s.metrics != nil {
		s.metrics.ObserveResendRequests(appmetrics.ResendLiveDrop, len(asked))
	}
}

// ask sends each target its request, the waits spread evenly over resendSpread, and returns the
// machines a request reached.
func (s *AgentServer) ask(targets []resendTarget) []uuid.UUID {
	asked := make([]uuid.UUID, 0, len(targets))
	for i, target := range targets {
		wait := minRetryAfter + resendSpread*time.Duration(i)/time.Duration(len(targets))
		if err := target.conn.SendResendRequest(context.Background(), target.since, wait); err != nil {
			s.logger.Debug("re-send request not sent", "device_id", target.conn.DeviceID, "error", err)
			continue
		}
		asked = append(asked, target.conn.DeviceID)
	}
	return asked
}
