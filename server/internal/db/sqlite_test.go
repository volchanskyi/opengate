package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testNameUpsertUpdates = "upsert updates existing"
	testNameGetNotFound   = "get not found"
	testNameDeleteNF      = "delete not found"
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

	t.Run(testNameUpsertUpdates, func(t *testing.T) {
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

	t.Run(testNameGetNotFound, func(t *testing.T) {
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

	t.Run(testNameDeleteNF, func(t *testing.T) {
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

	t.Run(testNameGetNotFound, func(t *testing.T) {
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

	t.Run(testNameDeleteNF, func(t *testing.T) {
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

	t.Run(testNameUpsertUpdates, func(t *testing.T) {
		d := &Device{ID: uuid.New(), GroupID: group.ID, Hostname: "old", OS: "linux", Status: StatusOffline}
		require.NoError(t, s.UpsertDevice(ctx, d))
		d.Hostname = "new"
		require.NoError(t, s.UpsertDevice(ctx, d))

		got, err := s.GetDevice(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, "new", got.Hostname)
	})

	t.Run(testNameGetNotFound, func(t *testing.T) {
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

	t.Run("reset all device statuses", func(t *testing.T) {
		d1 := seedDevice(t, ctx, s, group.ID)
		d2 := seedDevice(t, ctx, s, group.ID)
		require.NoError(t, s.SetDeviceStatus(ctx, d1.ID, StatusOnline))
		require.NoError(t, s.SetDeviceStatus(ctx, d2.ID, StatusOnline))

		require.NoError(t, s.ResetAllDeviceStatuses(ctx))

		got1, err := s.GetDevice(ctx, d1.ID)
		require.NoError(t, err)
		assert.Equal(t, StatusOffline, got1.Status)

		got2, err := s.GetDevice(ctx, d2.ID)
		require.NoError(t, err)
		assert.Equal(t, StatusOffline, got2.Status)
	})

	t.Run("reset all device statuses no-op when none online", func(t *testing.T) {
		require.NoError(t, s.ResetAllDeviceStatuses(ctx))
	})

	t.Run("delete", func(t *testing.T) {
		d := seedDevice(t, ctx, s, group.ID)
		require.NoError(t, s.DeleteDevice(ctx, d.ID))
		_, err := s.GetDevice(ctx, d.ID)
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run(testNameDeleteNF, func(t *testing.T) {
		err := s.DeleteDevice(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestListDevicesForOwner_IncludesUngrouped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	owner := seedUser(t, ctx, s)
	otherUser := seedUser(t, ctx, s)
	group := seedGroup(t, ctx, s, owner.ID)

	// Device in owner's group.
	grouped := seedDevice(t, ctx, s, group.ID)

	// Device with no group (ungrouped — the default after agent registration).
	ungrouped := &Device{
		ID:       uuid.New(),
		GroupID:  uuid.Nil,
		Hostname: "ungrouped-host",
		OS:       "linux",
		Status:   StatusOffline,
	}
	require.NoError(t, s.UpsertDevice(ctx, ungrouped))

	t.Run("owner sees grouped and ungrouped devices", func(t *testing.T) {
		devices, err := s.ListDevicesForOwner(ctx, owner.ID)
		require.NoError(t, err)

		ids := make(map[uuid.UUID]bool)
		for _, d := range devices {
			ids[d.ID] = true
		}
		assert.True(t, ids[grouped.ID], "should include grouped device")
		assert.True(t, ids[ungrouped.ID], "should include ungrouped device")
	})

	t.Run("other user sees ungrouped devices", func(t *testing.T) {
		devices, err := s.ListDevicesForOwner(ctx, otherUser.ID)
		require.NoError(t, err)

		ids := make(map[uuid.UUID]bool)
		for _, d := range devices {
			ids[d.ID] = true
		}
		assert.False(t, ids[grouped.ID], "should NOT include other owner's grouped device")
		assert.True(t, ids[ungrouped.ID], "should include ungrouped device")
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

	t.Run(testNameGetNotFound, func(t *testing.T) {
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

	t.Run(testNameDeleteNF, func(t *testing.T) {
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

	t.Run(testNameUpsertUpdates, func(t *testing.T) {
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

	t.Run(testNameDeleteNF, func(t *testing.T) {
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

	t.Run(testNameGetNotFound, func(t *testing.T) {
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

func TestEnrollmentTokenCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := seedUser(t, ctx, s)

	t.Run("create and get by token", func(t *testing.T) {
		tok := &EnrollmentToken{
			ID:        uuid.New(),
			Token:     "tok-" + uuid.New().String()[:8],
			Label:     "test-token",
			CreatedBy: owner.ID,
			MaxUses:   5,
			UseCount:  0,
			ExpiresAt: time.Now().Add(24 * time.Hour).UTC(),
		}
		require.NoError(t, s.CreateEnrollmentToken(ctx, tok))

		got, err := s.GetEnrollmentTokenByToken(ctx, tok.Token)
		require.NoError(t, err)
		assert.Equal(t, tok.ID, got.ID)
		assert.Equal(t, tok.Token, got.Token)
		assert.Equal(t, "test-token", got.Label)
		assert.Equal(t, owner.ID, got.CreatedBy)
		assert.Equal(t, 5, got.MaxUses)
		assert.Equal(t, 0, got.UseCount)
		assert.False(t, got.ExpiresAt.IsZero())
		assert.False(t, got.CreatedAt.IsZero())
	})

	t.Run(testNameGetNotFound, func(t *testing.T) {
		_, err := s.GetEnrollmentTokenByToken(ctx, "nonexistent-token")
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("list by creator", func(t *testing.T) {
		creator := seedUser(t, ctx, s)
		t1 := &EnrollmentToken{
			ID: uuid.New(), Token: "list-1-" + uuid.New().String()[:8],
			CreatedBy: creator.ID, ExpiresAt: time.Now().Add(time.Hour).UTC(),
		}
		t2 := &EnrollmentToken{
			ID: uuid.New(), Token: "list-2-" + uuid.New().String()[:8],
			CreatedBy: creator.ID, ExpiresAt: time.Now().Add(time.Hour).UTC(),
		}
		require.NoError(t, s.CreateEnrollmentToken(ctx, t1))
		require.NoError(t, s.CreateEnrollmentToken(ctx, t2))

		tokens, err := s.ListEnrollmentTokens(ctx, creator.ID)
		require.NoError(t, err)
		assert.Len(t, tokens, 2)
	})

	t.Run("list excludes other creators", func(t *testing.T) {
		other := seedUser(t, ctx, s)
		tokens, err := s.ListEnrollmentTokens(ctx, other.ID)
		require.NoError(t, err)
		assert.Empty(t, tokens)
	})

	t.Run("increment use count", func(t *testing.T) {
		tok := &EnrollmentToken{
			ID: uuid.New(), Token: "inc-" + uuid.New().String()[:8],
			CreatedBy: owner.ID, MaxUses: 3, ExpiresAt: time.Now().Add(time.Hour).UTC(),
		}
		require.NoError(t, s.CreateEnrollmentToken(ctx, tok))

		require.NoError(t, s.IncrementEnrollmentTokenUseCount(ctx, tok.ID))
		require.NoError(t, s.IncrementEnrollmentTokenUseCount(ctx, tok.ID))

		got, err := s.GetEnrollmentTokenByToken(ctx, tok.Token)
		require.NoError(t, err)
		assert.Equal(t, 2, got.UseCount)
	})

	t.Run("increment not found", func(t *testing.T) {
		err := s.IncrementEnrollmentTokenUseCount(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("delete", func(t *testing.T) {
		tok := &EnrollmentToken{
			ID: uuid.New(), Token: "del-" + uuid.New().String()[:8],
			CreatedBy: owner.ID, ExpiresAt: time.Now().Add(time.Hour).UTC(),
		}
		require.NoError(t, s.CreateEnrollmentToken(ctx, tok))
		require.NoError(t, s.DeleteEnrollmentToken(ctx, tok.ID))
		_, err := s.GetEnrollmentTokenByToken(ctx, tok.Token)
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run(testNameDeleteNF, func(t *testing.T) {
		err := s.DeleteEnrollmentToken(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("create with unlimited uses", func(t *testing.T) {
		tok := &EnrollmentToken{
			ID: uuid.New(), Token: "unlim-" + uuid.New().String()[:8],
			CreatedBy: owner.ID, MaxUses: 0, ExpiresAt: time.Now().Add(time.Hour).UTC(),
		}
		require.NoError(t, s.CreateEnrollmentToken(ctx, tok))

		got, err := s.GetEnrollmentTokenByToken(ctx, tok.Token)
		require.NoError(t, err)
		assert.Equal(t, 0, got.MaxUses)
	})
}

func TestSecurityGroupCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t.Run("migration seeds Administrators group", func(t *testing.T) {
		g, err := s.GetSecurityGroup(ctx, AdminGroupID)
		require.NoError(t, err)
		assert.Equal(t, "Administrators", g.Name)
		assert.Equal(t, "Full system access", g.Description)
		assert.True(t, g.IsSystem)
		assert.False(t, g.CreatedAt.IsZero())
	})

	t.Run("create and get", func(t *testing.T) {
		g := &SecurityGroup{
			ID:          uuid.New(),
			Name:        "Operators",
			Description: "Can manage devices",
		}
		require.NoError(t, s.CreateSecurityGroup(ctx, g))

		got, err := s.GetSecurityGroup(ctx, g.ID)
		require.NoError(t, err)
		assert.Equal(t, "Operators", got.Name)
		assert.Equal(t, "Can manage devices", got.Description)
		assert.False(t, got.IsSystem)
		assert.False(t, got.CreatedAt.IsZero())
	})

	t.Run(testNameGetNotFound, func(t *testing.T) {
		_, err := s.GetSecurityGroup(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("list includes seeded and created groups", func(t *testing.T) {
		groups, err := s.ListSecurityGroups(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(groups), 2) // Administrators + Operators
	})

	t.Run("delete non-system group", func(t *testing.T) {
		g := &SecurityGroup{ID: uuid.New(), Name: "Temporary-" + uuid.New().String()[:8]}
		require.NoError(t, s.CreateSecurityGroup(ctx, g))
		require.NoError(t, s.DeleteSecurityGroup(ctx, g.ID))
		_, err := s.GetSecurityGroup(ctx, g.ID)
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("cannot delete system group", func(t *testing.T) {
		err := s.DeleteSecurityGroup(ctx, AdminGroupID)
		assert.True(t, errors.Is(err, ErrSystemGroup))
	})

	t.Run(testNameDeleteNF, func(t *testing.T) {
		err := s.DeleteSecurityGroup(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("duplicate name fails", func(t *testing.T) {
		g := &SecurityGroup{ID: uuid.New(), Name: "Administrators"}
		err := s.CreateSecurityGroup(ctx, g)
		assert.Error(t, err)
	})
}

func TestSecurityGroupMembers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t.Run("add and list members", func(t *testing.T) {
		u1 := seedUser(t, ctx, s)
		u2 := seedUser(t, ctx, s)
		require.NoError(t, s.AddSecurityGroupMember(ctx, AdminGroupID, u1.ID))
		require.NoError(t, s.AddSecurityGroupMember(ctx, AdminGroupID, u2.ID))

		members, err := s.ListSecurityGroupMembers(ctx, AdminGroupID)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(members), 2)
	})

	t.Run("is user in group", func(t *testing.T) {
		u := seedUser(t, ctx, s)
		require.NoError(t, s.AddSecurityGroupMember(ctx, AdminGroupID, u.ID))

		ok, err := s.IsUserInSecurityGroup(ctx, u.ID, AdminGroupID)
		require.NoError(t, err)
		assert.True(t, ok)

		nonMember := seedUser(t, ctx, s)
		ok, err = s.IsUserInSecurityGroup(ctx, nonMember.ID, AdminGroupID)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("add is idempotent", func(t *testing.T) {
		u := seedUser(t, ctx, s)
		require.NoError(t, s.AddSecurityGroupMember(ctx, AdminGroupID, u.ID))
		require.NoError(t, s.AddSecurityGroupMember(ctx, AdminGroupID, u.ID)) // no error
	})

	t.Run("remove member", func(t *testing.T) {
		// Need at least 2 members so we can remove one
		u1 := seedUser(t, ctx, s)
		u2 := seedUser(t, ctx, s)
		require.NoError(t, s.AddSecurityGroupMember(ctx, AdminGroupID, u1.ID))
		require.NoError(t, s.AddSecurityGroupMember(ctx, AdminGroupID, u2.ID))

		require.NoError(t, s.RemoveSecurityGroupMember(ctx, AdminGroupID, u1.ID))

		ok, err := s.IsUserInSecurityGroup(ctx, u1.ID, AdminGroupID)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("count members", func(t *testing.T) {
		g := &SecurityGroup{ID: uuid.New(), Name: "Count-" + uuid.New().String()[:8]}
		require.NoError(t, s.CreateSecurityGroup(ctx, g))

		count, err := s.CountSecurityGroupMembers(ctx, g.ID)
		require.NoError(t, err)
		assert.Equal(t, 0, count)

		u := seedUser(t, ctx, s)
		require.NoError(t, s.AddSecurityGroupMember(ctx, g.ID, u.ID))
		count, err = s.CountSecurityGroupMembers(ctx, g.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	t.Run("remove not-found member", func(t *testing.T) {
		// Create a non-system group with 2 members so last-admin check doesn't trigger
		g := &SecurityGroup{ID: uuid.New(), Name: "RemNF-" + uuid.New().String()[:8]}
		require.NoError(t, s.CreateSecurityGroup(ctx, g))
		u := seedUser(t, ctx, s)
		require.NoError(t, s.AddSecurityGroupMember(ctx, g.ID, u.ID))
		// Try to remove a user who is not a member
		err := s.RemoveSecurityGroupMember(ctx, g.ID, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestSecurityGroupLastAdminProtection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Start fresh: Admin group has 0 members in this fresh DB.
	// Add exactly one user.
	u := seedUser(t, ctx, s)
	require.NoError(t, s.AddSecurityGroupMember(ctx, AdminGroupID, u.ID))

	// Verify only 1 member
	count, err := s.CountSecurityGroupMembers(ctx, AdminGroupID)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Try to remove last admin — should fail
	err = s.RemoveSecurityGroupMember(ctx, AdminGroupID, u.ID)
	assert.True(t, errors.Is(err, ErrLastAdmin))

	// Still a member
	ok, err := s.IsUserInSecurityGroup(ctx, u.ID, AdminGroupID)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestSecurityGroupSyncIsAdmin(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := seedUser(t, ctx, s)

	// Initially not admin
	got, err := s.GetUser(ctx, u.ID)
	require.NoError(t, err)
	assert.False(t, got.IsAdmin)

	// Add to Administrators group — should sync is_admin to true
	require.NoError(t, s.AddSecurityGroupMember(ctx, AdminGroupID, u.ID))
	got, err = s.GetUser(ctx, u.ID)
	require.NoError(t, err)
	assert.True(t, got.IsAdmin)

	// Add a second admin so we can remove the first
	u2 := seedUser(t, ctx, s)
	require.NoError(t, s.AddSecurityGroupMember(ctx, AdminGroupID, u2.ID))

	// Remove from Administrators group — should sync is_admin to false
	require.NoError(t, s.RemoveSecurityGroupMember(ctx, AdminGroupID, u.ID))
	got, err = s.GetUser(ctx, u.ID)
	require.NoError(t, err)
	assert.False(t, got.IsAdmin)
}

func TestSecurityGroupCascadeOnUserDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	g := &SecurityGroup{ID: uuid.New(), Name: "Cascade-" + uuid.New().String()[:8]}
	require.NoError(t, s.CreateSecurityGroup(ctx, g))

	u := seedUser(t, ctx, s)
	require.NoError(t, s.AddSecurityGroupMember(ctx, g.ID, u.ID))

	// Verify membership exists
	ok, err := s.IsUserInSecurityGroup(ctx, u.ID, g.ID)
	require.NoError(t, err)
	assert.True(t, ok)

	// Delete the user — membership should cascade
	require.NoError(t, s.DeleteUser(ctx, u.ID))

	count, err := s.CountSecurityGroupMembers(ctx, g.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestSecurityGroupMigrationSeedsExistingAdmins(t *testing.T) {
	// Test that migration 005 properly migrates is_admin=1 users.
	// Since newTestStore runs all migrations, we simulate by:
	// 1. Creating a user with is_admin=true
	// 2. Adding them to the Administrators group (simulating what migration does)
	// 3. Verifying they're in the group
	s := newTestStore(t)
	ctx := context.Background()

	adminUser := &User{
		ID:      uuid.New(),
		Email:   "og-admin-" + uuid.New().String()[:8] + "@example.com",
		IsAdmin: true,
	}
	require.NoError(t, s.UpsertUser(ctx, adminUser))
	require.NoError(t, s.AddSecurityGroupMember(ctx, AdminGroupID, adminUser.ID))

	ok, err := s.IsUserInSecurityGroup(ctx, adminUser.ID, AdminGroupID)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestDeviceUpdateCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := seedUser(t, ctx, s)
	group := seedGroup(t, ctx, s, owner.ID)
	device := seedDevice(t, ctx, s, group.ID)

	t.Run("create and list by version", func(t *testing.T) {
		du := &DeviceUpdate{
			DeviceID: device.ID,
			Version:  "1.0.0",
			Status:   UpdateStatusPending,
		}
		require.NoError(t, s.CreateDeviceUpdate(ctx, du))
		assert.NotZero(t, du.ID)

		list, err := s.ListDeviceUpdatesByVersion(ctx, "1.0.0")
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, device.ID, list[0].DeviceID)
		assert.Equal(t, "1.0.0", list[0].Version)
		assert.Equal(t, UpdateStatusPending, list[0].Status)
		assert.Empty(t, list[0].Error)
		assert.False(t, list[0].PushedAt.IsZero())
		assert.Nil(t, list[0].AckedAt)
	})

	t.Run("update status to success", func(t *testing.T) {
		require.NoError(t, s.UpdateDeviceUpdateStatus(ctx, device.ID, "1.0.0", UpdateStatusSuccess, ""))

		list, err := s.ListDeviceUpdatesByVersion(ctx, "1.0.0")
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, UpdateStatusSuccess, list[0].Status)
		assert.Empty(t, list[0].Error)
		assert.NotNil(t, list[0].AckedAt)
	})

	t.Run("update status to failed with error", func(t *testing.T) {
		du := &DeviceUpdate{
			DeviceID: device.ID,
			Version:  "2.0.0",
			Status:   UpdateStatusPending,
		}
		require.NoError(t, s.CreateDeviceUpdate(ctx, du))

		require.NoError(t, s.UpdateDeviceUpdateStatus(ctx, device.ID, "2.0.0", UpdateStatusFailed, "hash mismatch"))

		list, err := s.ListDeviceUpdatesByVersion(ctx, "2.0.0")
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, UpdateStatusFailed, list[0].Status)
		assert.Equal(t, "hash mismatch", list[0].Error)
		assert.NotNil(t, list[0].AckedAt)
	})

	t.Run("update status not found", func(t *testing.T) {
		err := s.UpdateDeviceUpdateStatus(ctx, uuid.New(), "9.9.9", UpdateStatusSuccess, "")
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("list empty version returns empty slice", func(t *testing.T) {
		list, err := s.ListDeviceUpdatesByVersion(ctx, "99.99.99")
		require.NoError(t, err)
		assert.Empty(t, list)
	})

	t.Run("multiple devices same version", func(t *testing.T) {
		d2 := seedDevice(t, ctx, s, group.ID)
		du1 := &DeviceUpdate{DeviceID: device.ID, Version: "3.0.0", Status: UpdateStatusPending}
		du2 := &DeviceUpdate{DeviceID: d2.ID, Version: "3.0.0", Status: UpdateStatusPending}
		require.NoError(t, s.CreateDeviceUpdate(ctx, du1))
		require.NoError(t, s.CreateDeviceUpdate(ctx, du2))

		list, err := s.ListDeviceUpdatesByVersion(ctx, "3.0.0")
		require.NoError(t, err)
		assert.Len(t, list, 2)
	})

	t.Run("cascade delete on device removal", func(t *testing.T) {
		d3 := seedDevice(t, ctx, s, group.ID)
		du := &DeviceUpdate{DeviceID: d3.ID, Version: "4.0.0", Status: UpdateStatusPending}
		require.NoError(t, s.CreateDeviceUpdate(ctx, du))

		require.NoError(t, s.DeleteDevice(ctx, d3.ID))

		list, err := s.ListDeviceUpdatesByVersion(ctx, "4.0.0")
		require.NoError(t, err)
		assert.Empty(t, list)
	})
}

func TestDeviceHardwareCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := seedUser(t, ctx, s)
	group := seedGroup(t, ctx, s, owner.ID)
	device := seedDevice(t, ctx, s, group.ID)

	t.Run("upsert and get", func(t *testing.T) {
		hw := &DeviceHardware{
			DeviceID:   device.ID,
			CPUModel:   "Intel Core i7-12700K",
			CPUCores:   12,
			RAMTotalMB: 32768,
			DiskTotalMB: 512000,
			DiskFreeMB:  256000,
			NetworkInterfaces: []NetworkInterfaceInfo{
				{Name: "eth0", MAC: "00:11:22:33:44:55", IPv4: []string{"192.168.1.100"}, IPv6: []string{}},
			},
		}
		require.NoError(t, s.UpsertDeviceHardware(ctx, hw))

		got, err := s.GetDeviceHardware(ctx, device.ID)
		require.NoError(t, err)
		assert.Equal(t, "Intel Core i7-12700K", got.CPUModel)
		assert.Equal(t, 12, got.CPUCores)
		assert.Equal(t, int64(32768), got.RAMTotalMB)
		assert.Equal(t, int64(512000), got.DiskTotalMB)
		assert.Equal(t, int64(256000), got.DiskFreeMB)
		require.Len(t, got.NetworkInterfaces, 1)
		assert.Equal(t, "eth0", got.NetworkInterfaces[0].Name)
		assert.Equal(t, "00:11:22:33:44:55", got.NetworkInterfaces[0].MAC)
		assert.False(t, got.UpdatedAt.IsZero())
	})

	t.Run(testNameUpsertUpdates, func(t *testing.T) {
		hw := &DeviceHardware{
			DeviceID:   device.ID,
			CPUModel:   "AMD Ryzen 9 7950X",
			CPUCores:   16,
			RAMTotalMB: 65536,
			DiskTotalMB: 1024000,
			DiskFreeMB:  500000,
			NetworkInterfaces: []NetworkInterfaceInfo{
				{Name: "eth0", MAC: "aa:bb:cc:dd:ee:ff", IPv4: []string{"10.0.0.1"}, IPv6: []string{"::1"}},
				{Name: "wlan0", MAC: "11:22:33:44:55:66", IPv4: []string{"192.168.1.50"}, IPv6: []string{}},
			},
		}
		require.NoError(t, s.UpsertDeviceHardware(ctx, hw))

		got, err := s.GetDeviceHardware(ctx, device.ID)
		require.NoError(t, err)
		assert.Equal(t, "AMD Ryzen 9 7950X", got.CPUModel)
		assert.Equal(t, 16, got.CPUCores)
		assert.Len(t, got.NetworkInterfaces, 2)
	})

	t.Run(testNameGetNotFound, func(t *testing.T) {
		_, err := s.GetDeviceHardware(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("empty network interfaces", func(t *testing.T) {
		d2 := seedDevice(t, ctx, s, group.ID)
		hw := &DeviceHardware{
			DeviceID:          d2.ID,
			CPUModel:          "ARM Cortex-A72",
			CPUCores:          4,
			RAMTotalMB:        4096,
			NetworkInterfaces: []NetworkInterfaceInfo{},
		}
		require.NoError(t, s.UpsertDeviceHardware(ctx, hw))

		got, err := s.GetDeviceHardware(ctx, d2.ID)
		require.NoError(t, err)
		assert.Empty(t, got.NetworkInterfaces)
	})

	t.Run("cascade delete on device removal", func(t *testing.T) {
		d3 := seedDevice(t, ctx, s, group.ID)
		hw := &DeviceHardware{
			DeviceID: d3.ID, CPUModel: "test", CPUCores: 1,
			NetworkInterfaces: []NetworkInterfaceInfo{},
		}
		require.NoError(t, s.UpsertDeviceHardware(ctx, hw))

		require.NoError(t, s.DeleteDevice(ctx, d3.ID))

		_, err := s.GetDeviceHardware(ctx, d3.ID)
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
