package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// postgresTestURLEnv is the env var pointing to an already-running Postgres
// instance that tests may connect to. When unset, all postgres_test.go tests
// are skipped. In CI, the env var is set by a service container.
const postgresTestURLEnv = "POSTGRES_TEST_URL"

// Shared test literals. Extracted to satisfy SonarCloud's "duplicated string
// literals" rule (go:S1192) without sprinkling nolint comments through the file.
const (
	tcUpsertAndGet          = "upsert and get"
	tcUpsertUpdatesExisting = "upsert updates existing"
	tcGetNotFound           = "get not found"
	tcDeleteNotFound        = "delete not found"
	tcCreateAndGet          = "create and get"

	pgTestLogTimestamp = "2026-04-10T10:00:00Z"
	pgTestVersionV123  = "v1.2.3"
	pgTestVersionV200  = "v2.0.0"
)

// Shared Postgres store across all tests in this file. Using a single
// long-lived store (rather than a per-test schema) lets us reset state via
// static-literal TRUNCATE + seed statements — dynamic DDL like
// `CREATE SCHEMA <name>` trips SonarCloud's go:S2077 hotspot rule and has
// no way to be parameterized in Postgres (identifiers cannot use $n binds).
//
// Tests in this file do not call t.Parallel(), so sequential reuse is safe.
var (
	pgTestStoreOnce sync.Once
	pgTestStore     *PostgresStore
	pgTestStoreErr  error
)

// resetPGSQL truncates every user table and re-seeds the Administrators
// security group (normally inserted by migration 001_initial.up.sql). The
// statement is a single static literal so Sonar's go:S2077 analyzer
// recognizes it as safe static SQL.
const resetPGSQL = `
TRUNCATE TABLE
	device_logs,
	device_hardware,
	device_updates,
	security_group_members,
	security_groups,
	enrollment_tokens,
	amt_devices,
	audit_events,
	web_push_subscriptions,
	agent_sessions,
	devices,
	groups_,
	users
RESTART IDENTITY CASCADE;

INSERT INTO security_groups (id, name, description, is_system)
VALUES ('00000000-0000-0000-0000-000000000001', 'Administrators', 'Full system access', TRUE)
ON CONFLICT DO NOTHING;
`

// newPostgresTestStore returns a shared PostgresStore with all user tables
// truncated and the built-in Administrators security group re-seeded, giving
// each test function the same clean slate as a fresh migration run.
//
// The env var POSTGRES_TEST_URL is mandatory; tests skip when it is unset.
func newPostgresTestStore(t *testing.T) *PostgresStore {
	t.Helper()

	baseURL := os.Getenv(postgresTestURLEnv)
	if baseURL == "" {
		t.Skipf("%s not set; skipping Postgres tests", postgresTestURLEnv)
	}

	pgTestStoreOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		pgTestStore, pgTestStoreErr = NewPostgresStore(ctx, baseURL)
	})
	require.NoError(t, pgTestStoreErr)
	require.NotNil(t, pgTestStore)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := pgTestStore.db.ExecContext(ctx, resetPGSQL)
	require.NoError(t, err, "reset postgres test state")

	return pgTestStore
}

// seedUserPG inserts a user into the given Postgres store and returns it.
func seedUserPG(t *testing.T, ctx context.Context, s *PostgresStore) *User {
	t.Helper()
	u := &User{
		ID:           uuid.New(),
		Email:        "pg-" + uuid.New().String()[:8] + "@example.com",
		PasswordHash: "hash",
		DisplayName:  "PG Test User",
	}
	require.NoError(t, s.UpsertUser(ctx, u))
	return u
}

// seedGroupPG inserts a group owned by ownerID and returns it.
func seedGroupPG(t *testing.T, ctx context.Context, s *PostgresStore, ownerID UserID) *Group {
	t.Helper()
	g := &Group{
		ID:      uuid.New(),
		Name:    "pg-group-" + uuid.New().String()[:8],
		OwnerID: ownerID,
	}
	require.NoError(t, s.CreateGroup(ctx, g))
	return g
}

// seedDevicePG inserts a device in the given group and returns it.
func seedDevicePG(t *testing.T, ctx context.Context, s *PostgresStore, groupID GroupID) *Device {
	t.Helper()
	d := &Device{
		ID:       uuid.New(),
		GroupID:  groupID,
		Hostname: "pg-host-" + uuid.New().String()[:8],
		OS:       "linux",
		Status:   StatusOffline,
	}
	require.NoError(t, s.UpsertDevice(ctx, d))
	return d
}

func TestPostgresPingAndClose(t *testing.T) {
	s := newPostgresTestStore(t)
	require.NoError(t, s.Ping(context.Background()))
}

func TestPostgresUserCRUD(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()

	t.Run(tcUpsertAndGet, func(t *testing.T) {
		u := &User{
			ID:           uuid.New(),
			Email:        "alice-pg@example.com",
			PasswordHash: "argon2",
			DisplayName:  "Alice PG",
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

	t.Run(tcUpsertUpdatesExisting, func(t *testing.T) {
		u := &User{ID: uuid.New(), Email: "update-pg@example.com", DisplayName: "Before"}
		require.NoError(t, s.UpsertUser(ctx, u))
		u.DisplayName = "After"
		require.NoError(t, s.UpsertUser(ctx, u))

		got, err := s.GetUser(ctx, u.ID)
		require.NoError(t, err)
		assert.Equal(t, "After", got.DisplayName)
	})

	t.Run("get by email", func(t *testing.T) {
		u := &User{ID: uuid.New(), Email: "byemail-pg@example.com"}
		require.NoError(t, s.UpsertUser(ctx, u))

		got, err := s.GetUserByEmail(ctx, "byemail-pg@example.com")
		require.NoError(t, err)
		assert.Equal(t, u.ID, got.ID)
	})

	t.Run("get by email not found", func(t *testing.T) {
		_, err := s.GetUserByEmail(ctx, "nope-pg@example.com")
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run(tcGetNotFound, func(t *testing.T) {
		_, err := s.GetUser(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("list users", func(t *testing.T) {
		users, err := s.ListUsers(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(users), 3)
	})

	t.Run("delete", func(t *testing.T) {
		u := &User{ID: uuid.New(), Email: "delete-pg@example.com"}
		require.NoError(t, s.UpsertUser(ctx, u))
		require.NoError(t, s.DeleteUser(ctx, u.ID))
		_, err := s.GetUser(ctx, u.ID)
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run(tcDeleteNotFound, func(t *testing.T) {
		err := s.DeleteUser(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestPostgresGroupCRUD(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()
	owner := seedUserPG(t, ctx, s)

	t.Run(tcCreateAndGet, func(t *testing.T) {
		g := &Group{ID: uuid.New(), Name: "Engineering PG", OwnerID: owner.ID}
		require.NoError(t, s.CreateGroup(ctx, g))

		got, err := s.GetGroup(ctx, g.ID)
		require.NoError(t, err)
		assert.Equal(t, "Engineering PG", got.Name)
		assert.Equal(t, owner.ID, got.OwnerID)
		assert.False(t, got.CreatedAt.IsZero())
	})

	t.Run("list groups by owner", func(t *testing.T) {
		other := seedUserPG(t, ctx, s)
		g1 := &Group{ID: uuid.New(), Name: "Team A PG", OwnerID: owner.ID}
		g2 := &Group{ID: uuid.New(), Name: "Team B PG", OwnerID: other.ID}
		require.NoError(t, s.CreateGroup(ctx, g1))
		require.NoError(t, s.CreateGroup(ctx, g2))

		groups, err := s.ListGroups(ctx, owner.ID)
		require.NoError(t, err)
		for _, g := range groups {
			assert.Equal(t, owner.ID, g.OwnerID)
		}
	})

	t.Run("delete", func(t *testing.T) {
		g := &Group{ID: uuid.New(), Name: "Delete PG", OwnerID: owner.ID}
		require.NoError(t, s.CreateGroup(ctx, g))
		require.NoError(t, s.DeleteGroup(ctx, g.ID))
		_, err := s.GetGroup(ctx, g.ID)
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run(tcDeleteNotFound, func(t *testing.T) {
		err := s.DeleteGroup(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestPostgresDeviceCRUD(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()
	owner := seedUserPG(t, ctx, s)
	group := seedGroupPG(t, ctx, s, owner.ID)

	t.Run(tcUpsertAndGet, func(t *testing.T) {
		d := &Device{
			ID:       uuid.New(),
			GroupID:  group.ID,
			Hostname: "workstation-pg",
			OS:       "linux",
			Status:   StatusOffline,
		}
		require.NoError(t, s.UpsertDevice(ctx, d))

		got, err := s.GetDevice(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, "workstation-pg", got.Hostname)
		assert.Equal(t, "linux", got.OS)
		assert.Equal(t, StatusOffline, got.Status)
		assert.Equal(t, group.ID, got.GroupID)
	})

	t.Run(tcUpsertUpdatesExisting, func(t *testing.T) {
		d := &Device{ID: uuid.New(), GroupID: group.ID, Hostname: "old-pg", OS: "linux", Status: StatusOffline}
		require.NoError(t, s.UpsertDevice(ctx, d))
		d.Hostname = "new-pg"
		require.NoError(t, s.UpsertDevice(ctx, d))

		got, err := s.GetDevice(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, "new-pg", got.Hostname)
	})

	t.Run("list devices by group", func(t *testing.T) {
		group2 := seedGroupPG(t, ctx, s, owner.ID)
		d1 := &Device{ID: uuid.New(), GroupID: group.ID, Hostname: "a-pg", OS: "linux", Status: StatusOffline}
		d2 := &Device{ID: uuid.New(), GroupID: group2.ID, Hostname: "b-pg", OS: "linux", Status: StatusOffline}
		require.NoError(t, s.UpsertDevice(ctx, d1))
		require.NoError(t, s.UpsertDevice(ctx, d2))

		devices, err := s.ListDevices(ctx, group2.ID)
		require.NoError(t, err)
		assert.Len(t, devices, 1)
		assert.Equal(t, "b-pg", devices[0].Hostname)
	})

	t.Run("list all devices", func(t *testing.T) {
		all, err := s.ListAllDevices(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(all), 2)
	})

	t.Run("set device status", func(t *testing.T) {
		d := seedDevicePG(t, ctx, s, group.ID)
		require.NoError(t, s.SetDeviceStatus(ctx, d.ID, StatusOnline))
		got, err := s.GetDevice(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, StatusOnline, got.Status)
	})

	t.Run("set device status not found", func(t *testing.T) {
		err := s.SetDeviceStatus(ctx, uuid.New(), StatusOnline)
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("reset all device statuses", func(t *testing.T) {
		d := seedDevicePG(t, ctx, s, group.ID)
		require.NoError(t, s.SetDeviceStatus(ctx, d.ID, StatusOnline))

		require.NoError(t, s.ResetAllDeviceStatuses(ctx))
		got, err := s.GetDevice(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, StatusOffline, got.Status)
	})

	t.Run("update device group", func(t *testing.T) {
		d := seedDevicePG(t, ctx, s, group.ID)
		group2 := seedGroupPG(t, ctx, s, owner.ID)
		require.NoError(t, s.UpdateDeviceGroup(ctx, d.ID, group2.ID))
		got, err := s.GetDevice(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, group2.ID, got.GroupID)
	})

	t.Run("delete", func(t *testing.T) {
		d := seedDevicePG(t, ctx, s, group.ID)
		require.NoError(t, s.DeleteDevice(ctx, d.ID))
		_, err := s.GetDevice(ctx, d.ID)
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run(tcDeleteNotFound, func(t *testing.T) {
		err := s.DeleteDevice(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestPostgresListDevicesForOwner(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()

	owner := seedUserPG(t, ctx, s)
	otherUser := seedUserPG(t, ctx, s)
	group := seedGroupPG(t, ctx, s, owner.ID)

	grouped := seedDevicePG(t, ctx, s, group.ID)

	ungrouped := &Device{
		ID:       uuid.New(),
		GroupID:  uuid.Nil,
		Hostname: "ungrouped-pg",
		OS:       "linux",
		Status:   StatusOffline,
	}
	require.NoError(t, s.UpsertDevice(ctx, ungrouped))

	t.Run("owner sees grouped and ungrouped", func(t *testing.T) {
		devices, err := s.ListDevicesForOwner(ctx, owner.ID)
		require.NoError(t, err)

		ids := make(map[uuid.UUID]bool)
		for _, d := range devices {
			ids[d.ID] = true
		}
		assert.True(t, ids[grouped.ID])
		assert.True(t, ids[ungrouped.ID])
	})

	t.Run("other user sees only ungrouped", func(t *testing.T) {
		devices, err := s.ListDevicesForOwner(ctx, otherUser.ID)
		require.NoError(t, err)

		ids := make(map[uuid.UUID]bool)
		for _, d := range devices {
			ids[d.ID] = true
		}
		assert.False(t, ids[grouped.ID])
		assert.True(t, ids[ungrouped.ID])
	})
}

func TestPostgresAgentSessionCRUD(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()
	owner := seedUserPG(t, ctx, s)
	group := seedGroupPG(t, ctx, s, owner.ID)
	device := seedDevicePG(t, ctx, s, group.ID)

	t.Run(tcCreateAndGet, func(t *testing.T) {
		sess := &AgentSession{
			Token:    "pg-tok-" + uuid.New().String()[:8],
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

	t.Run(tcGetNotFound, func(t *testing.T) {
		_, err := s.GetAgentSession(ctx, "nope-pg")
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("list active sessions for device", func(t *testing.T) {
		device2 := seedDevicePG(t, ctx, s, group.ID)
		s1 := &AgentSession{Token: "pg-s1-" + uuid.New().String()[:8], DeviceID: device2.ID, UserID: owner.ID}
		s2 := &AgentSession{Token: "pg-s2-" + uuid.New().String()[:8], DeviceID: device2.ID, UserID: owner.ID}
		require.NoError(t, s.CreateAgentSession(ctx, s1))
		require.NoError(t, s.CreateAgentSession(ctx, s2))

		sessions, err := s.ListActiveSessionsForDevice(ctx, device2.ID)
		require.NoError(t, err)
		assert.Len(t, sessions, 2)
	})

	t.Run("delete", func(t *testing.T) {
		sess := &AgentSession{Token: "pg-del-" + uuid.New().String()[:8], DeviceID: device.ID, UserID: owner.ID}
		require.NoError(t, s.CreateAgentSession(ctx, sess))
		require.NoError(t, s.DeleteAgentSession(ctx, sess.Token))
		_, err := s.GetAgentSession(ctx, sess.Token)
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run(tcDeleteNotFound, func(t *testing.T) {
		err := s.DeleteAgentSession(ctx, "nope-pg")
		assert.True(t, errors.Is(err, ErrNotFound))
	})

	t.Run("cascade delete on device removal", func(t *testing.T) {
		d := seedDevicePG(t, ctx, s, group.ID)
		sess := &AgentSession{Token: "pg-cascade-" + uuid.New().String()[:8], DeviceID: d.ID, UserID: owner.ID}
		require.NoError(t, s.CreateAgentSession(ctx, sess))
		require.NoError(t, s.DeleteDevice(ctx, d.ID))
		_, err := s.GetAgentSession(ctx, sess.Token)
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestPostgresWebPushSubscription(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()
	owner := seedUserPG(t, ctx, s)

	t.Run("upsert and list", func(t *testing.T) {
		sub := &WebPushSubscription{
			Endpoint: "https://push.example.com/pg-" + uuid.New().String()[:8],
			UserID:   owner.ID,
			P256dh:   "key",
			Auth:     "auth",
		}
		require.NoError(t, s.UpsertWebPushSubscription(ctx, sub))
		subs, err := s.ListWebPushSubscriptions(ctx, owner.ID)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(subs), 1)
	})

	t.Run(tcUpsertUpdatesExisting, func(t *testing.T) {
		endpoint := "https://push.example.com/pg-upd-" + uuid.New().String()[:8]
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

	t.Run("list all subscriptions", func(t *testing.T) {
		all, err := s.ListAllWebPushSubscriptions(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(all), 1)
	})

	t.Run("delete", func(t *testing.T) {
		endpoint := "https://push.example.com/pg-del-" + uuid.New().String()[:8]
		sub := &WebPushSubscription{Endpoint: endpoint, UserID: owner.ID}
		require.NoError(t, s.UpsertWebPushSubscription(ctx, sub))
		require.NoError(t, s.DeleteWebPushSubscription(ctx, endpoint))
	})

	t.Run(tcDeleteNotFound, func(t *testing.T) {
		err := s.DeleteWebPushSubscription(ctx, "https://push.example.com/nope-pg")
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestPostgresAuditLog(t *testing.T) {
	s := newPostgresTestStore(t)
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
		require.NoError(t, s.WriteAuditEvent(ctx, &AuditEvent{UserID: uuid.New(), Action: "logout", Target: "session"}))
		events, err := s.QueryAuditLog(ctx, AuditQuery{Action: "logout", Limit: 100})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(events), 1)
		for _, ev := range events {
			assert.Equal(t, "logout", ev.Action)
		}
	})

	t.Run("query with offset pagination", func(t *testing.T) {
		u := uuid.New()
		for i := 0; i < 3; i++ {
			require.NoError(t, s.WriteAuditEvent(ctx, &AuditEvent{
				UserID: u,
				Action: fmt.Sprintf("action-%d", i),
				Target: "x",
			}))
		}

		page1, err := s.QueryAuditLog(ctx, AuditQuery{UserID: &u, Limit: 2, Offset: 0})
		require.NoError(t, err)
		assert.Len(t, page1, 2)

		page2, err := s.QueryAuditLog(ctx, AuditQuery{UserID: &u, Limit: 2, Offset: 2})
		require.NoError(t, err)
		assert.Len(t, page2, 1)
	})
}

func TestPostgresAMTDeviceCRUD(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()

	t.Run(tcUpsertAndGet, func(t *testing.T) {
		d := &AMTDevice{
			UUID:     uuid.New(),
			Hostname: "amt-host",
			Model:    "AMT-M1",
			Firmware: "16.0",
			Status:   StatusOffline,
		}
		require.NoError(t, s.UpsertAMTDevice(ctx, d))

		got, err := s.GetAMTDevice(ctx, d.UUID)
		require.NoError(t, err)
		assert.Equal(t, d.UUID, got.UUID)
		assert.Equal(t, d.Hostname, got.Hostname)
		assert.Equal(t, d.Model, got.Model)
		assert.Equal(t, d.Firmware, got.Firmware)
	})

	t.Run("upsert preserves non-empty fields", func(t *testing.T) {
		id := uuid.New()
		d1 := &AMTDevice{UUID: id, Hostname: "host-a", Model: "Model-X", Firmware: "1.0", Status: StatusOnline}
		require.NoError(t, s.UpsertAMTDevice(ctx, d1))
		// Upsert with empty fields — should preserve existing non-empty values.
		d2 := &AMTDevice{UUID: id, Status: StatusOffline}
		require.NoError(t, s.UpsertAMTDevice(ctx, d2))

		got, err := s.GetAMTDevice(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, "host-a", got.Hostname)
		assert.Equal(t, "Model-X", got.Model)
		assert.Equal(t, "1.0", got.Firmware)
		assert.Equal(t, StatusOffline, got.Status)
	})

	t.Run("list all", func(t *testing.T) {
		all, err := s.ListAMTDevices(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(all), 1)
	})

	t.Run("set status", func(t *testing.T) {
		d := &AMTDevice{UUID: uuid.New(), Status: StatusOffline}
		require.NoError(t, s.UpsertAMTDevice(ctx, d))
		require.NoError(t, s.SetAMTDeviceStatus(ctx, d.UUID, StatusOnline))

		got, err := s.GetAMTDevice(ctx, d.UUID)
		require.NoError(t, err)
		assert.Equal(t, StatusOnline, got.Status)
	})

	t.Run(tcGetNotFound, func(t *testing.T) {
		_, err := s.GetAMTDevice(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestPostgresEnrollmentTokens(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()
	owner := seedUserPG(t, ctx, s)

	t.Run(tcCreateAndGet, func(t *testing.T) {
		token := &EnrollmentToken{
			ID:        uuid.New(),
			Token:     "enroll-" + uuid.New().String()[:8],
			CreatedBy: owner.ID,
			ExpiresAt: time.Now().Add(time.Hour),
			MaxUses:   5,
		}
		require.NoError(t, s.CreateEnrollmentToken(ctx, token))

		got, err := s.GetEnrollmentTokenByToken(ctx, token.Token)
		require.NoError(t, err)
		assert.Equal(t, token.ID, got.ID)
		assert.Equal(t, 5, got.MaxUses)
		assert.Equal(t, 0, got.UseCount)
	})

	t.Run("list by creator", func(t *testing.T) {
		tokens, err := s.ListEnrollmentTokens(ctx, owner.ID)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(tokens), 1)
	})

	t.Run("increment use count", func(t *testing.T) {
		token := &EnrollmentToken{
			ID:        uuid.New(),
			Token:     "inc-" + uuid.New().String()[:8],
			CreatedBy: owner.ID,
			ExpiresAt: time.Now().Add(time.Hour),
			MaxUses:   3,
		}
		require.NoError(t, s.CreateEnrollmentToken(ctx, token))
		require.NoError(t, s.IncrementEnrollmentTokenUseCount(ctx, token.ID))

		got, err := s.GetEnrollmentTokenByToken(ctx, token.Token)
		require.NoError(t, err)
		assert.Equal(t, 1, got.UseCount)
	})

	t.Run("delete", func(t *testing.T) {
		token := &EnrollmentToken{
			ID:        uuid.New(),
			Token:     "del-" + uuid.New().String()[:8],
			CreatedBy: owner.ID,
			ExpiresAt: time.Now().Add(time.Hour),
		}
		require.NoError(t, s.CreateEnrollmentToken(ctx, token))
		require.NoError(t, s.DeleteEnrollmentToken(ctx, token.ID))
		_, err := s.GetEnrollmentTokenByToken(ctx, token.Token)
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestPostgresDeviceHardware(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()
	owner := seedUserPG(t, ctx, s)
	group := seedGroupPG(t, ctx, s, owner.ID)
	device := seedDevicePG(t, ctx, s, group.ID)

	t.Run(tcUpsertAndGet, func(t *testing.T) {
		hw := &DeviceHardware{
			DeviceID:    device.ID,
			CPUModel:    "Intel Xeon E-2388G",
			CPUCores:    8,
			RAMTotalMB:  32768,
			DiskTotalMB: 1024000,
			DiskFreeMB:  512000,
			NetworkInterfaces: []NetworkInterfaceInfo{
				{Name: "eth0", MAC: "aa:bb:cc:dd:ee:00", IPv4: []string{"10.0.0.2"}, IPv6: []string{}},
			},
		}
		require.NoError(t, s.UpsertDeviceHardware(ctx, hw))

		got, err := s.GetDeviceHardware(ctx, device.ID)
		require.NoError(t, err)
		assert.Equal(t, "Intel Xeon E-2388G", got.CPUModel)
		assert.Equal(t, 8, got.CPUCores)
		assert.Equal(t, int64(32768), got.RAMTotalMB)
		assert.Len(t, got.NetworkInterfaces, 1)
		assert.Equal(t, "eth0", got.NetworkInterfaces[0].Name)
		assert.False(t, got.UpdatedAt.IsZero())
	})

	t.Run("upsert replaces", func(t *testing.T) {
		d := seedDevicePG(t, ctx, s, group.ID)
		hw1 := &DeviceHardware{DeviceID: d.ID, CPUModel: "CPU-A", CPUCores: 4}
		require.NoError(t, s.UpsertDeviceHardware(ctx, hw1))
		hw2 := &DeviceHardware{DeviceID: d.ID, CPUModel: "CPU-B", CPUCores: 16}
		require.NoError(t, s.UpsertDeviceHardware(ctx, hw2))

		got, err := s.GetDeviceHardware(ctx, d.ID)
		require.NoError(t, err)
		assert.Equal(t, "CPU-B", got.CPUModel)
		assert.Equal(t, 16, got.CPUCores)
	})

	t.Run("empty network interfaces", func(t *testing.T) {
		d := seedDevicePG(t, ctx, s, group.ID)
		hw := &DeviceHardware{
			DeviceID:          d.ID,
			CPUModel:          "ARM",
			CPUCores:          4,
			RAMTotalMB:        4096,
			NetworkInterfaces: []NetworkInterfaceInfo{},
		}
		require.NoError(t, s.UpsertDeviceHardware(ctx, hw))

		got, err := s.GetDeviceHardware(ctx, d.ID)
		require.NoError(t, err)
		assert.Empty(t, got.NetworkInterfaces)
	})

	t.Run(tcGetNotFound, func(t *testing.T) {
		_, err := s.GetDeviceHardware(ctx, uuid.New())
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestPostgresDeviceLogs(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()
	owner := seedUserPG(t, ctx, s)
	group := seedGroupPG(t, ctx, s, owner.ID)
	device := seedDevicePG(t, ctx, s, group.ID)

	t.Run("upsert batch and query", func(t *testing.T) {
		entries := []DeviceLogEntry{
			{Timestamp: pgTestLogTimestamp, Level: "INFO", Target: "mesh_agent::main", Message: "first"},
			{Timestamp: "2026-04-10T10:01:00Z", Level: "WARN", Target: "mesh_agent::main", Message: "second"},
			{Timestamp: "2026-04-10T10:02:00Z", Level: "ERROR", Target: "mesh_agent::connection", Message: "third"},
		}
		require.NoError(t, s.UpsertDeviceLogs(ctx, device.ID, entries))

		logs, total, err := s.QueryDeviceLogs(ctx, device.ID, LogFilter{Limit: 10})
		require.NoError(t, err)
		assert.Len(t, logs, 3)
		assert.Equal(t, 3, total)
	})

	t.Run("query with level filter", func(t *testing.T) {
		filter := LogFilter{Level: "ERROR", Limit: 10}
		logs, total, err := s.QueryDeviceLogs(ctx, device.ID, filter)
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Len(t, logs, 1)
		assert.Equal(t, "third", logs[0].Message)
	})

	t.Run("query with pagination", func(t *testing.T) {
		logs, total, err := s.QueryDeviceLogs(ctx, device.ID, LogFilter{Limit: 2, Offset: 0})
		require.NoError(t, err)
		assert.Equal(t, 3, total)
		assert.Len(t, logs, 2)
	})

	t.Run("has recent logs", func(t *testing.T) {
		d := seedDevicePG(t, ctx, s, group.ID)
		has, err := s.HasRecentLogs(ctx, d.ID, time.Hour)
		require.NoError(t, err)
		assert.False(t, has)

		require.NoError(t, s.UpsertDeviceLogs(ctx, d.ID, []DeviceLogEntry{
			{Timestamp: pgTestLogTimestamp, Level: "INFO", Target: "agent", Message: "hi"},
		}))
		has, err = s.HasRecentLogs(ctx, d.ID, time.Hour)
		require.NoError(t, err)
		assert.True(t, has)
	})

	t.Run("upsert replaces prior logs", func(t *testing.T) {
		d := seedDevicePG(t, ctx, s, group.ID)
		require.NoError(t, s.UpsertDeviceLogs(ctx, d.ID, []DeviceLogEntry{
			{Timestamp: "2026-04-10T09:00:00Z", Level: "INFO", Target: "agent", Message: "old"},
		}))
		require.NoError(t, s.UpsertDeviceLogs(ctx, d.ID, []DeviceLogEntry{
			{Timestamp: pgTestLogTimestamp, Level: "INFO", Target: "agent", Message: "new"},
		}))

		logs, _, err := s.QueryDeviceLogs(ctx, d.ID, LogFilter{Limit: 10})
		require.NoError(t, err)
		require.Len(t, logs, 1)
		assert.Equal(t, "new", logs[0].Message)
	})
}

func TestPostgresDeviceUpdates(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()
	owner := seedUserPG(t, ctx, s)
	group := seedGroupPG(t, ctx, s, owner.ID)
	device := seedDevicePG(t, ctx, s, group.ID)

	t.Run("create and list by version", func(t *testing.T) {
		du := &DeviceUpdate{
			DeviceID: device.ID,
			Version:  pgTestVersionV123,
			Status:   UpdateStatusPending,
		}
		require.NoError(t, s.CreateDeviceUpdate(ctx, du))
		assert.Greater(t, du.ID, int64(0))

		list, err := s.ListDeviceUpdatesByVersion(ctx, pgTestVersionV123)
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, UpdateStatusPending, list[0].Status)
	})

	t.Run("update status", func(t *testing.T) {
		require.NoError(t, s.UpdateDeviceUpdateStatus(ctx, device.ID, pgTestVersionV123, UpdateStatusSuccess, ""))
		list, err := s.ListDeviceUpdatesByVersion(ctx, pgTestVersionV123)
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, UpdateStatusSuccess, list[0].Status)
		assert.NotNil(t, list[0].AckedAt)
	})

	t.Run("update status with error", func(t *testing.T) {
		du := &DeviceUpdate{
			DeviceID: device.ID,
			Version:  pgTestVersionV200,
			Status:   UpdateStatusPending,
		}
		require.NoError(t, s.CreateDeviceUpdate(ctx, du))
		require.NoError(t, s.UpdateDeviceUpdateStatus(ctx, device.ID, pgTestVersionV200, UpdateStatusFailed, "download error"))

		list, err := s.ListDeviceUpdatesByVersion(ctx, pgTestVersionV200)
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, UpdateStatusFailed, list[0].Status)
		assert.Equal(t, "download error", list[0].Error)
	})

	t.Run("update not found", func(t *testing.T) {
		err := s.UpdateDeviceUpdateStatus(ctx, device.ID, "nonexistent", UpdateStatusSuccess, "")
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestPostgresSecurityGroups(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()
	owner := seedUserPG(t, ctx, s)

	t.Run("administrators group exists at init", func(t *testing.T) {
		g, err := s.GetSecurityGroup(ctx, AdminGroupID)
		require.NoError(t, err)
		assert.Equal(t, "Administrators", g.Name)
		assert.True(t, g.IsSystem)
	})

	t.Run("create custom group", func(t *testing.T) {
		g := &SecurityGroup{
			ID:          uuid.New(),
			Name:        "Custom-" + uuid.New().String()[:8],
			Description: "Test group",
			IsSystem:    false,
		}
		require.NoError(t, s.CreateSecurityGroup(ctx, g))

		got, err := s.GetSecurityGroup(ctx, g.ID)
		require.NoError(t, err)
		assert.Equal(t, g.Name, got.Name)
		assert.False(t, got.IsSystem)
	})

	t.Run("list groups", func(t *testing.T) {
		list, err := s.ListSecurityGroups(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(list), 2) // Administrators + custom
	})

	t.Run("add and remove member", func(t *testing.T) {
		g := &SecurityGroup{ID: uuid.New(), Name: "Members-" + uuid.New().String()[:8]}
		require.NoError(t, s.CreateSecurityGroup(ctx, g))

		require.NoError(t, s.AddSecurityGroupMember(ctx, g.ID, owner.ID))

		// Adding again should be idempotent.
		require.NoError(t, s.AddSecurityGroupMember(ctx, g.ID, owner.ID))

		inGroup, err := s.IsUserInSecurityGroup(ctx, owner.ID, g.ID)
		require.NoError(t, err)
		assert.True(t, inGroup)

		count, err := s.CountSecurityGroupMembers(ctx, g.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		members, err := s.ListSecurityGroupMembers(ctx, g.ID)
		require.NoError(t, err)
		assert.Len(t, members, 1)
		assert.Equal(t, owner.ID, members[0].ID)

		require.NoError(t, s.RemoveSecurityGroupMember(ctx, g.ID, owner.ID))
		inGroup, err = s.IsUserInSecurityGroup(ctx, owner.ID, g.ID)
		require.NoError(t, err)
		assert.False(t, inGroup)
	})

	t.Run("administrators admin flag sync", func(t *testing.T) {
		u := seedUserPG(t, ctx, s)
		assert.False(t, u.IsAdmin)

		require.NoError(t, s.AddSecurityGroupMember(ctx, AdminGroupID, u.ID))
		got, err := s.GetUser(ctx, u.ID)
		require.NoError(t, err)
		assert.True(t, got.IsAdmin)
	})

	t.Run("cannot delete system group", func(t *testing.T) {
		err := s.DeleteSecurityGroup(ctx, AdminGroupID)
		assert.ErrorIs(t, err, ErrSystemGroup)
	})

	t.Run("delete custom group cascades members", func(t *testing.T) {
		g := &SecurityGroup{ID: uuid.New(), Name: "Cascade-" + uuid.New().String()[:8]}
		require.NoError(t, s.CreateSecurityGroup(ctx, g))
		u := seedUserPG(t, ctx, s)
		require.NoError(t, s.AddSecurityGroupMember(ctx, g.ID, u.ID))

		require.NoError(t, s.DeleteSecurityGroup(ctx, g.ID))
		_, err := s.GetSecurityGroup(ctx, g.ID)
		assert.True(t, errors.Is(err, ErrNotFound))
	})
}

func TestPostgresLastAdminProtection(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()

	// Fresh store — Administrators group is empty on init.
	u := seedUserPG(t, ctx, s)
	require.NoError(t, s.AddSecurityGroupMember(ctx, AdminGroupID, u.ID))

	count, err := s.CountSecurityGroupMembers(ctx, AdminGroupID)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	err = s.RemoveSecurityGroupMember(ctx, AdminGroupID, u.ID)
	assert.ErrorIs(t, err, ErrLastAdmin)

	// Still a member.
	ok, err := s.IsUserInSecurityGroup(ctx, u.ID, AdminGroupID)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestPostgresSecurityGroupCascadeOnUserDelete(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()

	g := &SecurityGroup{ID: uuid.New(), Name: "PG-Cascade-" + uuid.New().String()[:8]}
	require.NoError(t, s.CreateSecurityGroup(ctx, g))

	u := seedUserPG(t, ctx, s)
	require.NoError(t, s.AddSecurityGroupMember(ctx, g.ID, u.ID))

	require.NoError(t, s.DeleteUser(ctx, u.ID))

	count, err := s.CountSecurityGroupMembers(ctx, g.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestPostgresSize(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()

	size, err := s.Size(ctx)
	require.NoError(t, err)
	assert.Greater(t, size, int64(0))
}
