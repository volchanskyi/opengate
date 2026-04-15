package db

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	benchEmail = "bench@test.com"
	benchGroup = "bench-group"
)

func newBenchStore(b *testing.B) Store {
	b.Helper()
	if pgTestDB == nil {
		b.Skipf("%s not set; skipping Postgres benchmarks", postgresTestURLEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := truncatePostgresTestDB(ctx, pgTestDB); err != nil {
		b.Fatal(err)
	}
	return pgTestDB
}

func BenchmarkStore_UpsertDevice(b *testing.B) {
	store := newBenchStore(b)
	ctx := context.Background()

	// Seed required FK: user + group
	userID := uuid.New()
	_ = store.UpsertUser(ctx, &User{
		ID: userID, Email: benchEmail, PasswordHash: "h", DisplayName: "Bench",
	})
	groupID := uuid.New()
	_ = store.CreateGroup(ctx, &Group{ID: groupID, Name: benchGroup, OwnerID: userID})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := &Device{
			ID:       uuid.New(),
			GroupID:  groupID,
			Hostname: fmt.Sprintf("host-%d", i),
			OS:       "linux",
			Status:   StatusOnline,
		}
		_ = store.UpsertDevice(ctx, d)
	}
}

func BenchmarkStore_GetDevice(b *testing.B) {
	store := newBenchStore(b)
	ctx := context.Background()

	userID := uuid.New()
	_ = store.UpsertUser(ctx, &User{
		ID: userID, Email: benchEmail, PasswordHash: "h", DisplayName: "Bench",
	})
	groupID := uuid.New()
	_ = store.CreateGroup(ctx, &Group{ID: groupID, Name: benchGroup, OwnerID: userID})

	deviceID := uuid.New()
	_ = store.UpsertDevice(ctx, &Device{
		ID: deviceID, GroupID: groupID, Hostname: "host", OS: "linux", Status: StatusOnline,
	})

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = store.GetDevice(ctx, deviceID)
	}
}

func BenchmarkStore_ListDevices(b *testing.B) {
	store := newBenchStore(b)
	ctx := context.Background()

	userID := uuid.New()
	_ = store.UpsertUser(ctx, &User{
		ID: userID, Email: benchEmail, PasswordHash: "h", DisplayName: "Bench",
	})
	groupID := uuid.New()
	_ = store.CreateGroup(ctx, &Group{ID: groupID, Name: benchGroup, OwnerID: userID})

	// Seed 50 devices
	for i := 0; i < 50; i++ {
		_ = store.UpsertDevice(ctx, &Device{
			ID: uuid.New(), GroupID: groupID, Hostname: fmt.Sprintf("host-%d", i),
			OS: "linux", Status: StatusOnline,
		})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = store.ListDevices(ctx, groupID)
	}
}

func BenchmarkStore_SetDeviceStatus(b *testing.B) {
	store := newBenchStore(b)
	ctx := context.Background()

	userID := uuid.New()
	_ = store.UpsertUser(ctx, &User{
		ID: userID, Email: benchEmail, PasswordHash: "h", DisplayName: "Bench",
	})
	groupID := uuid.New()
	_ = store.CreateGroup(ctx, &Group{ID: groupID, Name: benchGroup, OwnerID: userID})

	deviceID := uuid.New()
	_ = store.UpsertDevice(ctx, &Device{
		ID: deviceID, GroupID: groupID, Hostname: "host", OS: "linux", Status: StatusOffline,
	})

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = store.SetDeviceStatus(ctx, deviceID, StatusOnline)
	}
}
