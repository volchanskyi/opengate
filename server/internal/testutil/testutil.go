// Package testutil provides shared test helpers for the OpenGate server test suite.
// It is intended to be imported only from _test.go files.
package testutil

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// NewTestStore creates a temporary SQLite store backed by a t.TempDir() database
// and registers a cleanup that closes it when the test ends.
func NewTestStore(t testing.TB) db.Store {
	t.Helper()
	store, err := db.NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

// SeedUser inserts a minimal user into the store and returns it.
// The email is randomised to avoid uniqueness conflicts across parallel tests.
func SeedUser(t testing.TB, ctx context.Context, s db.Store) *db.User {
	t.Helper()
	u := &db.User{
		ID:           uuid.New(),
		Email:        "test-" + uuid.New().String()[:8] + "@example.com",
		PasswordHash: "hash",
		DisplayName:  "Test User",
	}
	require.NoError(t, s.UpsertUser(ctx, u))
	return u
}

// SeedGroup inserts a group owned by ownerID into the store and returns it.
func SeedGroup(t testing.TB, ctx context.Context, s db.Store, ownerID uuid.UUID) *db.Group {
	t.Helper()
	g := &db.Group{
		ID:      uuid.New(),
		Name:    "group-" + uuid.New().String()[:8],
		OwnerID: ownerID,
	}
	require.NoError(t, s.CreateGroup(ctx, g))
	return g
}

// SeedDevice inserts an offline device belonging to groupID into the store and returns it.
func SeedDevice(t testing.TB, ctx context.Context, s db.Store, groupID uuid.UUID) *db.Device {
	t.Helper()
	d := &db.Device{
		ID:       uuid.New(),
		GroupID:  groupID,
		Hostname: "host-" + uuid.New().String()[:8],
		OS:       "linux",
		Status:   db.StatusOffline,
	}
	require.NoError(t, s.UpsertDevice(ctx, d))
	return d
}

// SeedAgentSession inserts an agent session for the given device and user.
func SeedAgentSession(t testing.TB, ctx context.Context, s db.Store, deviceID, userID uuid.UUID) *db.AgentSession {
	t.Helper()
	sess := &db.AgentSession{
		Token:    string(protocol.GenerateSessionToken()),
		DeviceID: deviceID,
		UserID:   userID,
	}
	require.NoError(t, s.CreateAgentSession(ctx, sess))
	return sess
}

// SeedAdminUser inserts an admin user with a real bcrypt password hash.
func SeedAdminUser(t testing.TB, ctx context.Context, s db.Store) (*db.User, string) {
	t.Helper()
	password := "admin-pass-" + uuid.New().String()[:8]
	hash, err := auth.HashPassword(password)
	require.NoError(t, err)
	u := &db.User{
		ID:           uuid.New(),
		Email:        "admin-" + uuid.New().String()[:8] + "@example.com",
		PasswordHash: hash,
		DisplayName:  "Admin User",
		IsAdmin:      true,
	}
	require.NoError(t, s.UpsertUser(ctx, u))
	return u, password
}

// SeedAMTDevice inserts an AMT device record into the store.
func SeedAMTDevice(t testing.TB, ctx context.Context, s db.Store) *db.AMTDevice {
	t.Helper()
	d := &db.AMTDevice{
		UUID:     uuid.New(),
		Hostname: "amt-" + uuid.New().String()[:8],
		Model:    "Intel NUC",
		Firmware: "16.1.0",
		Status:   db.StatusOffline,
		LastSeen: time.Now(),
	}
	require.NoError(t, s.UpsertAMTDevice(ctx, d))
	return d
}

// GenerateJWT creates a JWT token for the given user using the provided config.
func GenerateJWT(t testing.TB, cfg *auth.JWTConfig, user *db.User) string {
	t.Helper()
	token, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)
	return token
}
