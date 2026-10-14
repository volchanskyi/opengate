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

// ErrSessionNotFound is returned by Delete when no session in the caller's tenant matches.
var ErrSessionNotFound = errors.New("session not found")

// ErrForbidden is returned by Delete when the caller neither holds the session nor administers.
var ErrForbidden = errors.New("only the session's holder or an administrator may end it")

// SessionEnder ends a live relay session, telling both of its sides why.
type SessionEnder interface {
	End(token protocol.SessionToken, reason string) bool
}

// SessionService orchestrates ending a session across the session store, the relay, the
// notifier and the audit log.
type SessionService struct {
	sessions session.Repository
	notifier notifications.Notifier
	audit    audit.Repository
	relay    SessionEnder
}

// NewSessionService wires the session use case; relay ends the live session a deleted row named.
func NewSessionService(
	sessions session.Repository,
	notifier notifications.Notifier,
	auditRepo audit.Repository,
	relay SessionEnder,
) *SessionService {
	return &SessionService{sessions: sessions, notifier: notifier, audit: auditRepo, relay: relay}
}

// DeleteSessionInput is the input to SessionService.Delete.
type DeleteSessionInput struct {
	// Token names the session; when empty, DeviceID names the device whose one session ends.
	Token    string
	DeviceID uuid.UUID
	// UserID is the caller, recorded on the audit event and the session-ended notification.
	UserID uuid.UUID
	// ActorName names the caller in the reason the holder's tab shows.
	ActorName string
	// IsAdmin lets the caller end a session another technician holds.
	IsAdmin bool
}

// Delete ends a session: the row goes, the live relay closes with "Ended by <name>", and an audit
// event and a push notification follow. Only the holder or an administrator may end it.
func (s *SessionService) Delete(ctx context.Context, in DeleteSessionInput) error {
	held, err := s.find(ctx, in)
	if err != nil {
		return err
	}
	if held.UserID != in.UserID && !in.IsAdmin {
		return ErrForbidden
	}

	if err := s.sessions.Delete(ctx, held.Token); err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return ErrSessionNotFound
		}
		return err
	}
	s.relay.End(protocol.SessionToken(held.Token), "Ended by "+in.ActorName)

	// Audit and notification failures do not fail the delete.
	_ = s.audit.Write(ctx, &audit.Event{
		UserID: in.UserID,
		Action: "session.delete",
		Target: protocol.RedactToken(held.Token),
	})
	_ = s.notifier.Notify(ctx, notifications.Event{
		Type:      notifications.EventSessionEnded,
		UserID:    in.UserID,
		Timestamp: time.Now(),
	})

	return nil
}

// find resolves the session Delete names, by token or by device.
func (s *SessionService) find(ctx context.Context, in DeleteSessionInput) (*session.Session, error) {
	var held *session.Session
	var err error
	if in.Token != "" {
		held, err = s.sessions.Get(ctx, in.Token)
	} else {
		held, err = s.sessions.ForDevice(ctx, in.DeviceID)
	}
	if errors.Is(err, session.ErrSessionNotFound) {
		return nil, ErrSessionNotFound
	}
	return held, err
}
