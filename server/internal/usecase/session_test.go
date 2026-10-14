package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/usecase"
)

type fakeSessions struct {
	stored    map[string]*session.Session
	deleteErr error
}

func (f *fakeSessions) Create(_ context.Context, s *session.Session) (string, error) {
	if f.stored == nil {
		f.stored = map[string]*session.Session{}
	}
	f.stored[s.Token] = s
	return "", nil
}
func (f *fakeSessions) ForDevice(_ context.Context, deviceID uuid.UUID) (*session.Session, error) {
	for _, s := range f.stored {
		if s.DeviceID == deviceID {
			return s, nil
		}
	}
	return nil, session.ErrSessionNotFound
}
func (f *fakeSessions) MarkOutcome(context.Context, string, int) (bool, error) {
	return true, nil
}
func (f *fakeSessions) Get(_ context.Context, token string) (*session.Session, error) {
	s, ok := f.stored[token]
	if !ok {
		return nil, session.ErrSessionNotFound
	}
	return s, nil
}
func (f *fakeSessions) Delete(_ context.Context, token string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.stored, token)
	return nil
}
func (f *fakeSessions) DeleteRelaySession(ctx context.Context, token string) error {
	return f.Delete(ctx, token)
}
func (f *fakeSessions) ListActiveForDevice(context.Context, uuid.UUID) ([]*session.Session, error) {
	return nil, nil
}
func (f *fakeSessions) DeleteStale(context.Context, time.Time, []string) (int, error) {
	return 0, nil
}

// fakeEnder records each live session the use case ended and the reason it gave.
type fakeEnder struct{ ended map[string]string }

func (f *fakeEnder) End(token protocol.SessionToken, reason string) bool {
	if f.ended == nil {
		f.ended = map[string]string{}
	}
	f.ended[string(token)] = reason
	return true
}

type fakeNotifier struct{ events []notifications.Event }

func (f *fakeNotifier) Notify(_ context.Context, e notifications.Event) error {
	f.events = append(f.events, e)
	return nil
}
func (f *fakeNotifier) VAPIDPublicKey() string { return "" }

type fakeAudit struct{ events []*audit.Event }

func (f *fakeAudit) Write(_ context.Context, e *audit.Event) error {
	f.events = append(f.events, e)
	return nil
}
func (f *fakeAudit) Query(context.Context, audit.Query) ([]*audit.Event, error) {
	return nil, nil
}

func TestSessionService_Delete_NotFound(t *testing.T) {
	svc := usecase.NewSessionService(&fakeSessions{stored: map[string]*session.Session{}},
		&fakeNotifier{}, &fakeAudit{}, &fakeEnder{})

	err := svc.Delete(context.Background(), usecase.DeleteSessionInput{
		Token:  "nonexistent",
		UserID: uuid.New(),
	})
	require.ErrorIs(t, err, usecase.ErrSessionNotFound)

	err = svc.Delete(context.Background(), usecase.DeleteSessionInput{
		DeviceID: uuid.New(),
		UserID:   uuid.New(),
	})
	require.ErrorIs(t, err, usecase.ErrSessionNotFound, "a free host has no session to end")
}

func TestSessionService_Delete_AnotherTechnicianIsRefused(t *testing.T) {
	holder := uuid.New()
	sess := &fakeSessions{stored: map[string]*session.Session{"t": {Token: "t", UserID: holder}}}
	ender := &fakeEnder{}
	svc := usecase.NewSessionService(sess, &fakeNotifier{}, &fakeAudit{}, ender)

	err := svc.Delete(context.Background(), usecase.DeleteSessionInput{
		Token:     "t",
		UserID:    uuid.New(),
		ActorName: "Bob",
	})

	require.ErrorIs(t, err, usecase.ErrForbidden)
	require.Contains(t, sess.stored, "t", "the holder's session carries on")
	require.Empty(t, ender.ended)
}

func TestSessionService_Delete_AnAdministratorEndsAnotherTechniciansSession(t *testing.T) {
	holder, admin := uuid.New(), uuid.New()
	device := uuid.New()
	sess := &fakeSessions{stored: map[string]*session.Session{
		"t": {Token: "t", UserID: holder, DeviceID: device},
	}}
	ender := &fakeEnder{}
	audit := &fakeAudit{}
	svc := usecase.NewSessionService(sess, &fakeNotifier{}, audit, ender)

	err := svc.Delete(context.Background(), usecase.DeleteSessionInput{
		DeviceID:  device,
		UserID:    admin,
		ActorName: "Carol",
		IsAdmin:   true,
	})

	require.NoError(t, err)
	require.NotContains(t, sess.stored, "t")
	require.Equal(t, map[string]string{"t": "Ended by Carol"}, ender.ended,
		"the live session ends and its tab is told who ended it")
	require.Len(t, audit.events, 1)
	require.Equal(t, admin, audit.events[0].UserID, "the audit event names the caller, not the holder")
}

func TestSessionService_Delete_HappyPath_AuditsAndNotifies(t *testing.T) {
	creator := uuid.New()
	sess := &fakeSessions{stored: map[string]*session.Session{"t": {Token: "t", UserID: creator}}}
	notif := &fakeNotifier{}
	audit := &fakeAudit{}
	ender := &fakeEnder{}
	svc := usecase.NewSessionService(sess, notif, audit, ender)

	err := svc.Delete(context.Background(), usecase.DeleteSessionInput{
		Token:     "t",
		UserID:    creator,
		ActorName: "Alice",
	})

	require.NoError(t, err)
	require.NotContains(t, sess.stored, "t", "session must be deleted")
	require.Equal(t, map[string]string{"t": "Ended by Alice"}, ender.ended)
	require.Len(t, audit.events, 1, "audit Event must be written")
	require.Equal(t, "session.delete", audit.events[0].Action)
	require.Len(t, notif.events, 1, "session.end event must be emitted")
	require.Equal(t, notifications.EventSessionEnded, notif.events[0].Type)
}

func TestSessionService_Delete_RepositoryError(t *testing.T) {
	creator := uuid.New()
	sess := &fakeSessions{
		stored:    map[string]*session.Session{"t": {Token: "t", UserID: creator}},
		deleteErr: errors.New("db down"),
	}
	ender := &fakeEnder{}
	svc := usecase.NewSessionService(sess, &fakeNotifier{}, &fakeAudit{}, ender)

	err := svc.Delete(context.Background(), usecase.DeleteSessionInput{
		Token:  "t",
		UserID: creator,
	})

	require.EqualError(t, err, "db down")
	require.Empty(t, ender.ended, "a session whose row survives keeps running")
}
