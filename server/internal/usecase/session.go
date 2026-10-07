// Package usecase holds orchestration that spans several aggregates, free of HTTP types.
package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/session"
)

// ErrSessionNotFound is returned by Delete when the token names no session in the caller's tenant.
var ErrSessionNotFound = errors.New("session not found")

// SessionService orchestrates session deletion across the session store, notifier and audit log.
type SessionService struct {
	sessions session.Repository
	notifier notifications.Notifier
	audit    audit.Repository
}

func NewSessionService(
	sessions session.Repository,
	notifier notifications.Notifier,
	auditRepo audit.Repository,
) *SessionService {
	return &SessionService{sessions: sessions, notifier: notifier, audit: auditRepo}
}

// DeleteSessionInput is the input to SessionService.Delete.
type DeleteSessionInput struct {
	Token string
	// UserID is recorded on the audit event and the session-ended notification.
	UserID uuid.UUID
}

// Delete removes a session and emits an audit event and a push notification.
// Tenant membership is the whole gate: any member may end any session in the tenant.
func (s *SessionService) Delete(ctx context.Context, in DeleteSessionInput) error {
	if _, err := s.sessions.Get(ctx, in.Token); err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	if err := s.sessions.Delete(ctx, in.Token); err != nil {
		return err
	}

	// Audit and notification failures do not fail the delete.
	_ = s.audit.Write(ctx, &audit.Event{
		UserID: in.UserID,
		Action: "session.delete",
		Target: protocol.RedactToken(in.Token),
	})
	_ = s.notifier.Notify(ctx, notifications.Event{
		Type:      notifications.EventSessionEnded,
		UserID:    in.UserID,
		Timestamp: time.Now(),
	})

	return nil
}
