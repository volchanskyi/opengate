// Package usecase holds cross-aggregate orchestration that doesn't belong
// at the transport layer and would violate per-aggregate leaf-module
// boundaries if pushed into a single domain module.
//
// Per ADR-019, transport handlers (api package) translate HTTP requests
// and responses to/from method calls on use-case services; the services
// compose per-aggregate Repository ports to deliver a domain-meaningful
// outcome. Use cases own NO HTTP types and are reusable from CLI/gRPC/
// in-process callers.
//
// Pilot scope (this commit): SessionService.Delete only. Other session
// methods (Create with 6-module orchestration, List) and other domains
// (device, auth, updater) remain in api/handlers_*.go until an
// opportunistic trigger moves them — same earned-port rule that
// ADR-020 §3.6 applies to leaf-module port extraction.
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

// Domain-level errors returned by SessionService. The transport layer
// maps these to HTTP status codes (404 / 403 / etc.).
var (
	// ErrSessionNotFound is returned by Delete when the session token
	// does not exist. Wraps session.ErrSessionNotFound.
	ErrSessionNotFound = errors.New("session not found")
	// ErrSessionForbidden is returned when the caller is neither the
	// session creator nor an admin.
	ErrSessionForbidden = errors.New("session forbidden")
)

// SessionService orchestrates the session aggregate's use cases. Pilot
// scope is Delete; Create and List remain in the transport layer until
// the orchestration cost of carving them earns the move.
//
// Composes leaf-domain ports directly (audit.Repository, session.Repository,
// notifications.Notifier). Per ADR-019, usecase is the only component
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
	// UserID is the caller's identity (from JWT claims).
	UserID uuid.UUID
	// IsAdmin signals the caller has the admin role; admins may delete
	// any session, regardless of creator.
	IsAdmin bool
}

// Delete removes a session and emits an audit log + push event. Returns
// ErrSessionNotFound if the token is unknown, ErrSessionForbidden if the
// caller is neither the creator nor admin, or the underlying Repository
// error on persistence failure.
func (s *SessionService) Delete(ctx context.Context, in DeleteSessionInput) error {
	sess, err := s.sessions.Get(ctx, in.Token)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	if sess.UserID != in.UserID && !in.IsAdmin {
		return ErrSessionForbidden
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
