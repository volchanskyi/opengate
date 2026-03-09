package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStore creates a temporary SQLite store for testing.
func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

// seedUser creates a user in the store for FK dependencies.
func seedUser(t *testing.T, ctx context.Context, s *SQLiteStore) *User {
	t.Helper()
	u := &User{
		ID:           uuid.New(),
		Email:        "test-" + uuid.New().String()[:8] + "@example.com",
		PasswordHash: "hash",
		DisplayName:  "Test User",
	}
	require.NoError(t, s.UpsertUser(ctx, u))
	return u
}

// seedGroup creates a group in the store for FK dependencies.
func seedGroup(t *testing.T, ctx context.Context, s *SQLiteStore, ownerID UserID) *Group {
	t.Helper()
	g := &Group{
		ID:      uuid.New(),
		Name:    "group-" + uuid.New().String()[:8],
		OwnerID: ownerID,
	}
	require.NoError(t, s.CreateGroup(ctx, g))
	return g
}

// seedDevice creates a device in the store for FK dependencies.
func seedDevice(t *testing.T, ctx context.Context, s *SQLiteStore, groupID GroupID) *Device {
	t.Helper()
	d := &Device{
		ID:       uuid.New(),
		GroupID:  groupID,
		Hostname: "host-" + uuid.New().String()[:8],
		OS:       "linux",
		Status:   StatusOffline,
	}
	require.NoError(t, s.UpsertDevice(ctx, d))
	return d
}

func TestNewSQLiteStore(t *testing.T) {
	t.Run("creates database and runs migrations", func(t *testing.T) {
		s := newTestStore(t)
		require.NoError(t, s.Ping(context.Background()))
	})

	t.Run("fails on invalid path", func(t *testing.T) {
		_, err := NewSQLiteStore("/nonexistent/dir/test.db")
		assert.Error(t, err)
	})

	t.Run("opens existing database", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "reopen.db")

		s1, err := NewSQLiteStore(dbPath)
		require.NoError(t, err)
		ctx := context.Background()
		u := &User{ID: uuid.New(), Email: "reopen@example.com"}
		require.NoError(t, s1.UpsertUser(ctx, u))
		require.NoError(t, s1.Close())

		s2, err := NewSQLiteStore(dbPath)
		require.NoError(t, err)
		defer s2.Close()
		got, err := s2.GetUser(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, u.Email, got.Email)
	})
}

func TestPing(t *testing.T) {
	s := newTestStore(t)
	assert.NoError(t, s.Ping(context.Background()))
}

func TestUserCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t.Run("upsert and get", func(t *testing.T) {
		u := &User{
			ID:           uuid.New(),
			Email:        "alice@example.com",
			PasswordHash: "argon2",
			DisplayName:  "Alice",
			IsAdmin:      true,
		}
		require.NoError(t, s.UpsertUser(ctx, u))

		got, err := s.GetUser(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, u.ID, got.ID)
		assert.Equal(t, u.Email, got.Email)
		assert.Equal(t, u.PasswordHash, got.PasswordHash)
		assert.Equal(t, u.DisplayName, got.DisplayName)
		assert.True(t, got.IsAdmin)
		assert.False(t, got.CreatedAt.IsZero())
		assert.False(t, got.UpdatedAt.IsZero())
	})

	t.Run("upsert updates existing", func(t *testing.T) {
		u := &User{ID: uuid.New(), Email: "update-me@example.com", DisplayName: "Before"}
		require.NoError(t, s.UpsertUser(ctx, u))

		u.DisplayName = "After"
		require.NoError(t, s.UpsertUser(ctx, u))

		got, err := s.GetUser(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, "After", got.DisplayName)
	})

	t.Run("get by email", func(t *testing.T) {
		u := &User{ID: uuid.New(), Email: "byemail@example.com"}
		require.NoError(t, s.UpsertUser(ctx, u))

		got, err := s.GetUserByEmail(ctx, "byemail@example.com")
		require.NoError(t, err)
		assert.Equal(t, u.ID, got.ID)
	})

	t.Run("get by email not found", func(t *testing.T) {
		_, err := s.GetUserByEmail(ctx, "nope@example.com")
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := s.GetUser(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("list users", func(t *testing.T) {
		users, err := s.ListUsers(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(users), 2) // from earlier subtests
	})

	t.Run("delete", func(t *testing.T) {
		u := &User{ID: uuid.New(), Email: "delete-me@example.com"}
		require.NoError(t, s.UpsertUser(ctx, u))
		require.NoError(t, s.DeleteUser(ctx, u.ID))
		_, err := s.GetUser(ctx, u.ID)
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("delete not found", func(t *testing.T) {
		err := s.DeleteUser(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestGroupCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := seedUser(t, ctx, s)

	t.Run("create and get", func(t *testing.T) {
		g := &Group{ID: uuid.New(), Name: "Engineering", OwnerID: owner.ID}
		require.NoError(t, s.CreateGroup(ctx, g))

		got, err := s.GetGroup(ctx, g.ID)
		require.NoError(t, err)
		assert.Equal(t, "Engineering", got.Name)
		assert.Equal(t, owner.ID, got.OwnerID)
		assert.False(t, got.CreatedAt.IsZero())
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := s.GetGroup(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("list groups by owner", func(t *testing.T) {
		other := seedUser(t, ctx, s)
		g1 := &Group{ID: uuid.New(), Name: "Team A", OwnerID: owner.ID}
		g2 := &Group{ID: uuid.New(), Name: "Team B", OwnerID: other.ID}
		require.NoError(t, s.CreateGroup(ctx, g1))
		require.NoError(t, s.CreateGroup(ctx, g2))

		groups, err := s.ListGroups(ctx, owner.ID)
		require.NoError(t, err)
		for _, g := range groups {
			assert.Equal(t, owner.ID, g.OwnerID)
		}

		otherGroups, err := s.ListGroups(ctx, other.ID)
		require.NoError(t, err)
		assert.Len(t, otherGroups, 1)
		assert.Equal(t, "Team B", otherGroups[0].Name)
	})

	t.Run("delete", func(t *testing.T) {
		g := &Group{ID: uuid.New(), Name: "Delete Me", OwnerID: owner.ID}
		require.NoError(t, s.CreateGroup(ctx, g))
		require.NoError(t, s.DeleteGroup(ctx, g.ID))
		_, err := s.GetGroup(ctx, g.ID)
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("delete not found", func(t *testing.T) {
		err := s.DeleteGroup(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestDeviceCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := seedUser(t, ctx, s)
	group := seedGroup(t, ctx, s, owner.ID)

	t.Run("upsert and get", func(t *testing.T) {
		d := &Device{
			ID:       uuid.New(),
			GroupID:  group.ID,
			Hostname: "workstation-01",
			OS:       "linux",
			Status:   StatusOffline,
		}
		require.NoError(t, s.UpsertDevice(ctx, d))

		got, err := s.GetDevice(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, "workstation-01", got.Hostname)
		assert.Equal(t, "linux", got.OS)
		assert.Equal(t, StatusOffline, got.Status)
		assert.Equal(t, group.ID, got.GroupID)
		assert.False(t, got.CreatedAt.IsZero())
	})

	t.Run("upsert updates existing", func(t *testing.T) {
		d := &Device{ID: uuid.New(), GroupID: group.ID, Hostname: "old", OS: "linux", Status: StatusOffline}
		require.NoError(t, s.UpsertDevice(ctx, d))
		d.Hostname = "new"
		require.NoError(t, s.UpsertDevice(ctx, d))

		got, err := s.GetDevice(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, "new", got.Hostname)
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := s.GetDevice(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("list devices by group", func(t *testing.T) {
		group2 := seedGroup(t, ctx, s, owner.ID)
		d1 := &Device{ID: uuid.New(), GroupID: group.ID, Hostname: "a", OS: "linux", Status: StatusOffline}
		d2 := &Device{ID: uuid.New(), GroupID: group2.ID, Hostname: "b", OS: "linux", Status: StatusOffline}
		require.NoError(t, s.UpsertDevice(ctx, d1))
		require.NoError(t, s.UpsertDevice(ctx, d2))

		devices, err := s.ListDevices(ctx, group2.ID)
		require.NoError(t, err)
		assert.Len(t, devices, 1)
		assert.Equal(t, "b", devices[0].Hostname)
	})

	t.Run("set device status", func(t *testing.T) {
		d := seedDevice(t, ctx, s, group.ID)
		require.NoError(t, s.SetDeviceStatus(ctx, d.ID, StatusOnline))

		got, err := s.GetDevice(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, StatusOnline, got.Status)
	})

	t.Run("set status not found", func(t *testing.T) {
		err := s.SetDeviceStatus(ctx, uuid.New(), StatusOnline)
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("delete", func(t *testing.T) {
		d := seedDevice(t, ctx, s, group.ID)
		require.NoError(t, s.DeleteDevice(ctx, d.ID))
		_, err := s.GetDevice(ctx, d.ID)
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("delete not found", func(t *testing.T) {
		err := s.DeleteDevice(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestAgentSessionCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := seedUser(t, ctx, s)
	group := seedGroup(t, ctx, s, owner.ID)
	device := seedDevice(t, ctx, s, group.ID)

	t.Run("create and get", func(t *testing.T) {
		sess := &AgentSession{
			Token:    "tok-" + uuid.New().String()[:8],
			DeviceID: device.ID,
			UserID:   owner.ID,
		}
		require.NoError(t, s.CreateAgentSession(ctx, sess))

		got, err := s.GetAgentSession(ctx, sess.Token)
		require.NoError(t, err)
		assert.Equal(t, sess.Token, got.Token)
		assert.Equal(t, device.ID, got.DeviceID)
		assert.Equal(t, owner.ID, got.UserID)
		assert.False(t, got.CreatedAt.IsZero())
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := s.GetAgentSession(ctx, "nonexistent")
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("list active sessions for device", func(t *testing.T) {
		device2 := seedDevice(t, ctx, s, group.ID)
		s1 := &AgentSession{Token: "s1-" + uuid.New().String()[:8], DeviceID: device2.ID, UserID: owner.ID}
		s2 := &AgentSession{Token: "s2-" + uuid.New().String()[:8], DeviceID: device2.ID, UserID: owner.ID}
		require.NoError(t, s.CreateAgentSession(ctx, s1))
		require.NoError(t, s.CreateAgentSession(ctx, s2))

		sessions, err := s.ListActiveSessionsForDevice(ctx, device2.ID)
		require.NoError(t, err)
		assert.Len(t, sessions, 2)
	})

	t.Run("delete", func(t *testing.T) {
		sess := &AgentSession{Token: "del-" + uuid.New().String()[:8], DeviceID: device.ID, UserID: owner.ID}
		require.NoError(t, s.CreateAgentSession(ctx, sess))
		require.NoError(t, s.DeleteAgentSession(ctx, sess.Token))
		_, err := s.GetAgentSession(ctx, sess.Token)
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("delete not found", func(t *testing.T) {
		err := s.DeleteAgentSession(ctx, "nope")
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("cascade delete on device removal", func(t *testing.T) {
		d := seedDevice(t, ctx, s, group.ID)
		sess := &AgentSession{Token: "cascade-" + uuid.New().String()[:8], DeviceID: d.ID, UserID: owner.ID}
		require.NoError(t, s.CreateAgentSession(ctx, sess))

		require.NoError(t, s.DeleteDevice(ctx, d.ID))
		_, err := s.GetAgentSession(ctx, sess.Token)
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestWebPushSubscriptionCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := seedUser(t, ctx, s)

	t.Run("upsert and list", func(t *testing.T) {
		sub := &WebPushSubscription{
			Endpoint: "https://push.example.com/" + uuid.New().String()[:8],
			UserID:   owner.ID,
			P256dh:   "key123",
			Auth:     "auth456",
		}
		require.NoError(t, s.UpsertWebPushSubscription(ctx, sub))

		subs, err := s.ListWebPushSubscriptions(ctx, owner.ID)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(subs), 1)

		found := false
		for _, got := range subs {
			if got.Endpoint == sub.Endpoint {
				assert.Equal(t, "key123", got.P256dh)
				assert.Equal(t, "auth456", got.Auth)
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("upsert updates existing", func(t *testing.T) {
		endpoint := "https://push.example.com/update-" + uuid.New().String()[:8]
		sub := &WebPushSubscription{Endpoint: endpoint, UserID: owner.ID, P256dh: "old", Auth: "old"}
		require.NoError(t, s.UpsertWebPushSubscription(ctx, sub))

		sub.P256dh = "new"
		require.NoError(t, s.UpsertWebPushSubscription(ctx, sub))

		subs, err := s.ListWebPushSubscriptions(ctx, owner.ID)
		require.NoError(t, err)
		for _, got := range subs {
			if got.Endpoint == endpoint {
				assert.Equal(t, "new", got.P256dh)
			}
		}
	})

	t.Run("delete", func(t *testing.T) {
		endpoint := "https://push.example.com/del-" + uuid.New().String()[:8]
		sub := &WebPushSubscription{Endpoint: endpoint, UserID: owner.ID}
		require.NoError(t, s.UpsertWebPushSubscription(ctx, sub))
		require.NoError(t, s.DeleteWebPushSubscription(ctx, endpoint))

		// Verify it's gone by listing (no direct get method)
		subs, err := s.ListWebPushSubscriptions(ctx, owner.ID)
		require.NoError(t, err)
		for _, got := range subs {
			assert.NotEqual(t, endpoint, got.Endpoint)
		}
	})

	t.Run("delete not found", func(t *testing.T) {
		err := s.DeleteWebPushSubscription(ctx, "https://push.example.com/nope")
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("cascade delete on user removal", func(t *testing.T) {
		u := seedUser(t, ctx, s)
		endpoint := "https://push.example.com/cascade-" + uuid.New().String()[:8]
		sub := &WebPushSubscription{Endpoint: endpoint, UserID: u.ID}
		require.NoError(t, s.UpsertWebPushSubscription(ctx, sub))

		require.NoError(t, s.DeleteUser(ctx, u.ID))
		subs, err := s.ListWebPushSubscriptions(ctx, u.ID)
		require.NoError(t, err)
		assert.Empty(t, subs)
	})
}

func TestAuditLog(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	userID := uuid.New()

	t.Run("write and query", func(t *testing.T) {
		event := &AuditEvent{
			UserID:  userID,
			Action:  "login",
			Target:  "session",
			Details: "from 10.0.0.1",
		}
		require.NoError(t, s.WriteAuditEvent(ctx, event))

		events, err := s.QueryAuditLog(ctx, AuditQuery{UserID: &userID, Limit: 10})
		require.NoError(t, err)
		require.Len(t, events, 1)
		assert.Equal(t, "login", events[0].Action)
		assert.Equal(t, "from 10.0.0.1", events[0].Details)
		assert.False(t, events[0].CreatedAt.IsZero())
		assert.Greater(t, events[0].ID, int64(0))
	})

	t.Run("query by action", func(t *testing.T) {
		e1 := &AuditEvent{UserID: userID, Action: "logout"}
		e2 := &AuditEvent{UserID: userID, Action: "login"}
		require.NoError(t, s.WriteAuditEvent(ctx, e1))
		require.NoError(t, s.WriteAuditEvent(ctx, e2))

		events, err := s.QueryAuditLog(ctx, AuditQuery{Action: "logout", Limit: 100})
		require.NoError(t, err)
		for _, e := range events {
			assert.Equal(t, "logout", e.Action)
		}
	})

	t.Run("query with limit and offset", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			require.NoError(t, s.WriteAuditEvent(ctx, &AuditEvent{UserID: userID, Action: "paginate"}))
		}

		page1, err := s.QueryAuditLog(ctx, AuditQuery{Action: "paginate", Limit: 2})
		require.NoError(t, err)
		assert.Len(t, page1, 2)

		page2, err := s.QueryAuditLog(ctx, AuditQuery{Action: "paginate", Limit: 2, Offset: 2})
		require.NoError(t, err)
		assert.Len(t, page2, 2)
		assert.NotEqual(t, page1[0].ID, page2[0].ID)
	})

	t.Run("query no filters returns all", func(t *testing.T) {
		events, err := s.QueryAuditLog(ctx, AuditQuery{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(events), 1)
	})
}

func TestCorruptUUID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := seedUser(t, ctx, s)
	group := seedGroup(t, ctx, s, owner.ID)

	// Insert a device row with a corrupt UUID directly via raw SQL.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO devices (id, group_id, hostname, os, status, last_seen, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"not-a-uuid", group.ID.String(), "corrupt-host", "linux", "offline",
		"2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")
	require.NoError(t, err)

	// ListDevices must return an error rather than panic when it scans the corrupt row.
	_, err = s.ListDevices(ctx, group.ID)
	assert.Error(t, err)
	assert.False(t, errors.Is(err, ErrNotFound))
}

func TestCorruptTimestamp(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := seedUser(t, ctx, s)
	group := seedGroup(t, ctx, s, owner.ID)

	deviceID := uuid.New()
	// Insert a device row with a corrupt timestamp directly via raw SQL.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO devices (id, group_id, hostname, os, status, last_seen, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		deviceID.String(), group.ID.String(), "ts-host", "linux", "offline",
		"not-a-time", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")
	require.NoError(t, err)

	_, err = s.GetDevice(ctx, deviceID)
	assert.Error(t, err)
	assert.False(t, errors.Is(err, ErrNotFound))
}

func TestAMTDeviceCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t.Run("upsert and get", func(t *testing.T) {
		d := &AMTDevice{
			UUID:     uuid.New(),
			Hostname: "amt-host-1",
			Model:    "vPro i7",
			Firmware: "16.1.0",
			Status:   StatusOnline,
		}
		require.NoError(t, s.UpsertAMTDevice(ctx, d))

		got, err := s.GetAMTDevice(ctx, d.UUID)
		require.NoError(t, err)
		assert.Equal(t, d.UUID, got.UUID)
		assert.Equal(t, "amt-host-1", got.Hostname)
		assert.Equal(t, "vPro i7", got.Model)
		assert.Equal(t, "16.1.0", got.Firmware)
		assert.Equal(t, StatusOnline, got.Status)
		assert.False(t, got.LastSeen.IsZero())
	})

	t.Run("upsert preserves non-empty fields", func(t *testing.T) {
		id := uuid.New()
		d := &AMTDevice{UUID: id, Hostname: "host-a", Model: "Model-X", Firmware: "1.0", Status: StatusOnline}
		require.NoError(t, s.UpsertAMTDevice(ctx, d))

		// Second upsert with empty strings should preserve existing values
		d2 := &AMTDevice{UUID: id, Status: StatusOffline}
		require.NoError(t, s.UpsertAMTDevice(ctx, d2))

		got, err := s.GetAMTDevice(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, "host-a", got.Hostname)
		assert.Equal(t, "Model-X", got.Model)
		assert.Equal(t, StatusOffline, got.Status)
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := s.GetAMTDevice(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("list", func(t *testing.T) {
		id1 := uuid.New()
		id2 := uuid.New()
		require.NoError(t, s.UpsertAMTDevice(ctx, &AMTDevice{UUID: id1, Hostname: "list-1", Status: StatusOnline}))
		require.NoError(t, s.UpsertAMTDevice(ctx, &AMTDevice{UUID: id2, Hostname: "list-2", Status: StatusOffline}))

		devices, err := s.ListAMTDevices(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(devices), 2)
	})

	t.Run("set status", func(t *testing.T) {
		d := &AMTDevice{UUID: uuid.New(), Status: StatusOnline}
		require.NoError(t, s.UpsertAMTDevice(ctx, d))

		require.NoError(t, s.SetAMTDeviceStatus(ctx, d.UUID, StatusOffline))
		got, err := s.GetAMTDevice(ctx, d.UUID)
		require.NoError(t, err)
		assert.Equal(t, StatusOffline, got.Status)
	})

	t.Run("set status not found", func(t *testing.T) {
		err := s.SetAMTDeviceStatus(ctx, uuid.New(), StatusOnline)
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestWALMode(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "wal.db")
	s, err := NewSQLiteStore(dbPath)
	require.NoError(t, err)
	defer s.Close()

	// WAL file should be created after first write
	ctx := context.Background()
	require.NoError(t, s.UpsertUser(ctx, &User{ID: uuid.New(), Email: "wal@test.com"}))

	_, err = os.Stat(dbPath + "-wal")
	assert.NoError(t, err, "WAL file should exist")
}
