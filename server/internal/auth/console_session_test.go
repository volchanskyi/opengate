package auth_test

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func consoleSessionFor(t *testing.T, user *auth.User, at time.Time) *auth.ConsoleSession {
	t.Helper()
	return &auth.ConsoleSession{
		IDHash:     sha256.Sum256([]byte(uuid.NewString())),
		UserID:     user.ID,
		TenantID:   user.TenantID,
		Email:      user.Email,
		IsAdmin:    true,
		CSRFToken:  uuid.NewString(),
		StartedAt:  at,
		LastSeenAt: at,
	}
}

func seededUser(t *testing.T, ctx context.Context, users auth.UserRepository) *auth.User {
	t.Helper()
	user := &auth.User{ID: uuid.New(), Email: "console-" + uuid.NewString()[:8] + "@example.com"}
	require.NoError(t, users.Upsert(ctx, user))
	return user
}

func TestPostgresConsoleSessionsKeepWhatTheSignInRecorded(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)
	user := seededUser(t, ctx, testutil.NewTestUsers(t, store))
	sessions := auth.NewPostgresConsoleSessions(store.DB())
	at := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

	session := consoleSessionFor(t, user, at)
	require.NoError(t, sessions.Create(context.Background(), session))

	got, err := sessions.Get(context.Background(), session.IDHash)
	require.NoError(t, err)
	assert.Equal(t, user.ID, got.UserID)
	assert.Equal(t, user.TenantID, got.TenantID)
	assert.Equal(t, user.Email, got.Email)
	assert.True(t, got.IsAdmin)
	assert.Equal(t, session.CSRFToken, got.CSRFToken)
	assert.True(t, got.StartedAt.Equal(at))

	require.NoError(t, sessions.Touch(context.Background(), session.IDHash, at.Add(5*time.Minute)))
	got, err = sessions.Get(context.Background(), session.IDHash)
	require.NoError(t, err)
	assert.True(t, got.LastSeenAt.Equal(at.Add(5*time.Minute)))
	assert.True(t, got.StartedAt.Equal(at), "a request renews the idle clock only")
}

func TestPostgresConsoleSessionsForgetASignedOutSession(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)
	user := seededUser(t, ctx, testutil.NewTestUsers(t, store))
	sessions := auth.NewPostgresConsoleSessions(store.DB())

	session := consoleSessionFor(t, user, time.Now())
	require.NoError(t, sessions.Create(context.Background(), session))
	require.NoError(t, sessions.Delete(context.Background(), session.IDHash))

	_, err := sessions.Get(context.Background(), session.IDHash)
	require.ErrorIs(t, err, auth.ErrConsoleSessionNotFound)
}

func TestPostgresConsoleSessionsSweepOnlyThePastOnes(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)
	user := seededUser(t, ctx, testutil.NewTestUsers(t, store))
	sessions := auth.NewPostgresConsoleSessions(store.DB())
	now := time.Date(2026, 10, 16, 18, 0, 0, 0, time.UTC)

	idle := consoleSessionFor(t, user, now.Add(-2*time.Hour))
	tooLong := consoleSessionFor(t, user, now.Add(-9*time.Hour))
	tooLong.LastSeenAt = now.Add(-time.Minute)
	live := consoleSessionFor(t, user, now.Add(-time.Hour))
	live.LastSeenAt = now.Add(-time.Minute)
	for _, s := range []*auth.ConsoleSession{idle, tooLong, live} {
		require.NoError(t, sessions.Create(context.Background(), s))
	}

	swept, err := sessions.DeleteExpired(context.Background(), now.Add(-30*time.Minute), now.Add(-8*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(2), swept)
	_, err = sessions.Get(context.Background(), live.IDHash)
	assert.NoError(t, err, "a session inside both limits stays")
}

func TestAConsoleSessionGoesWithItsUser(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), true)
	user := seededUser(t, ctx, testutil.NewTestUsers(t, store))
	sessions := auth.NewPostgresConsoleSessions(store.DB())
	session := consoleSessionFor(t, user, time.Now())
	require.NoError(t, sessions.Create(context.Background(), session))

	_, err := store.DB().ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, user.ID)
	require.NoError(t, err)

	_, err = sessions.Get(context.Background(), session.IDHash)
	require.ErrorIs(t, err, auth.ErrConsoleSessionNotFound)
}
