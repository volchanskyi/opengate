package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/usecase"
)

// fakeSessions is a minimal session.Repository stub for use-case tests.
type fakeSessions struct {
	stored    map[string]*session.Session
	deleteErr error
}

func (f *fakeSessions) Create(_ context.Context, s *session.Session) error {
	if f.stored == nil {
		f.stored = map[string]*session.Session{}
	}
	f.stored[s.Token] = s
	return nil
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
func (f *fakeSessions) ListActiveForDevice(context.Context, uuid.UUID) ([]*session.Session, error) {
	return nil, nil
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
	svc := usecase.NewSessionService(&fakeSessions{stored: map[string]*session.Session{}}, &fakeNotifier{}, &fakeAudit{})

	err := svc.Delete(context.Background(), usecase.DeleteSessionInput{
		Token:  "nonexistent",
		UserID: uuid.New(),
	})

	require.ErrorIs(t, err, usecase.ErrSessionNotFound)
}

func TestSessionService_Delete_Forbidden_NonCreatorNonAdmin(t *testing.T) {
	creator := uuid.New()
	caller := uuid.New()
	sess := &fakeSessions{stored: map[string]*session.Session{"t": {Token: "t", UserID: creator}}}
	svc := usecase.NewSessionService(sess, &fakeNotifier{}, &fakeAudit{})

	err := svc.Delete(context.Background(), usecase.DeleteSessionInput{
		Token:   "t",
		UserID:  caller,
		IsAdmin: false,
	})

	require.ErrorIs(t, err, usecase.ErrSessionForbidden)
	require.Contains(t, sess.stored, "t", "session must NOT be deleted on forbidden")
}

func TestSessionService_Delete_HappyPath_AuditsAndNotifies(t *testing.T) {
	creator := uuid.New()
	sess := &fakeSessions{stored: map[string]*session.Session{"t": {Token: "t", UserID: creator}}}
	notif := &fakeNotifier{}
	audit := &fakeAudit{}
	svc := usecase.NewSessionService(sess, notif, audit)

	err := svc.Delete(context.Background(), usecase.DeleteSessionInput{
		Token:  "t",
		UserID: creator,
	})

	require.NoError(t, err)
	require.NotContains(t, sess.stored, "t", "session must be deleted")
	require.Len(t, audit.events, 1, "audit Event must be written")
	require.Equal(t, "session.delete", audit.events[0].Action)
	require.Len(t, notif.events, 1, "session.end event must be emitted")
	require.Equal(t, notifications.EventSessionEnded, notif.events[0].Type)
}

func TestSessionService_Delete_AdminCanDeleteAnyone(t *testing.T) {
	creator := uuid.New()
	admin := uuid.New()
	sess := &fakeSessions{stored: map[string]*session.Session{"t": {Token: "t", UserID: creator}}}
	svc := usecase.NewSessionService(sess, &fakeNotifier{}, &fakeAudit{})

	err := svc.Delete(context.Background(), usecase.DeleteSessionInput{
		Token:   "t",
		UserID:  admin,
		IsAdmin: true,
	})

	require.NoError(t, err)
	require.NotContains(t, sess.stored, "t")
}

func TestSessionService_Delete_RepositoryError(t *testing.T) {
	creator := uuid.New()
	sess := &fakeSessions{
		stored:    map[string]*session.Session{"t": {Token: "t", UserID: creator}},
		deleteErr: errors.New("db down"),
	}
	svc := usecase.NewSessionService(sess, &fakeNotifier{}, &fakeAudit{})

	err := svc.Delete(context.Background(), usecase.DeleteSessionInput{
		Token:  "t",
		UserID: creator,
	})

	require.EqualError(t, err, "db down")
}
