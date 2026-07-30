// Package usecase holds cross-aggregate orchestration that doesn't belong
// at the transport layer and would violate per-aggregate leaf-module
// boundaries if pushed into a single domain module.
//
// Transport handlers in the api package translate HTTP requests
// and responses to/from method calls on use-case services; the services
// compose per-aggregate Repository ports to deliver a domain-meaningful
// outcome. Use cases own NO HTTP types and are reusable from CLI/gRPC/
// in-process callers.
//
// SessionService.Delete is the current cross-aggregate use case. Other methods
// remain in api/handlers_*.go because ports are extracted only when a concrete
// consumer boundary earns them.
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

// ErrSessionNotFound is returned by Delete when the session token does not
// exist in the caller's tenant scope. Wraps session.ErrSessionNotFound; the
// transport layer maps it to 404.
var ErrSessionNotFound = errors.New("session not found")

// SessionService orchestrates deletion across the session aggregate, notifier,
// and audit log. Create and List remain in the transport layer because their
// current orchestration does not earn a separate use-case boundary.
//
// Composes leaf-domain ports directly (audit.Repository, session.Repository,
// notifications.Notifier). The usecase package is the only component
// permitted to import multiple leaf aggregates.
type SessionService struct {
	sessions session.Repository
	notifier notifications.Notifier
	audit    audit.Repository
}

// NewSessionService wires SessionService against its outbound ports.
func NewSessionService(
	sessions session.Repository,
	notifier notifications.Notifier,
	auditRepo audit.Repository,
) *SessionService {
	return &SessionService{sessions: sessions, notifier: notifier, audit: auditRepo}
}

// DeleteSessionInput is the input to SessionService.Delete.
type DeleteSessionInput struct {
	// Token identifies the session to delete.
	Token string
	// UserID is the caller's identity (from JWT claims), recorded on the audit
	// event and the session-ended notification.
	UserID uuid.UUID
}

// Delete removes a session and emits an audit log + push event. Ending a remote
// session is a device command, so organization membership is the whole gate: the
// lookup runs in the caller's tenant scope and any member may end any session on
// a device in that organization. Returns ErrSessionNotFound if the token is
// unknown in scope, or the underlying Repository error on persistence failure.
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

	// Fire-and-forget audit write — failure is non-fatal for the delete.
	_ = s.audit.Write(ctx, &audit.Event{
		UserID: in.UserID,
		Action: "session.delete",
		Target: protocol.RedactToken(in.Token),
	})
	// Fire-and-forget notification — failures are non-fatal for the delete.
	_ = s.notifier.Notify(ctx, notifications.Event{
		Type:      notifications.EventSessionEnded,
		UserID:    in.UserID,
		Timestamp: time.Now(),
	})

	return nil
}
