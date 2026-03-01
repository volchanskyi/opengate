package db

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func newBenchStore(b *testing.B) *SQLiteStore {
	b.Helper()
	store, err := NewSQLiteStore(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { store.Close() })
	return store
}

func BenchmarkStore_UpsertDevice(b *testing.B) {
	store := newBenchStore(b)
	ctx := context.Background()

	// Seed required FK: user + group
	userID := uuid.New()
	_ = store.UpsertUser(ctx, &User{
		ID: userID, Email: "bench@test.com", PasswordHash: "h", DisplayName: "Bench",
	})
	groupID := uuid.New()
	_ = store.CreateGroup(ctx, &Group{ID: groupID, Name: "bench-group", OwnerID: userID})

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
		ID: userID, Email: "bench@test.com", PasswordHash: "h", DisplayName: "Bench",
	})
	groupID := uuid.New()
	_ = store.CreateGroup(ctx, &Group{ID: groupID, Name: "bench-group", OwnerID: userID})

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
		ID: userID, Email: "bench@test.com", PasswordHash: "h", DisplayName: "Bench",
	})
	groupID := uuid.New()
	_ = store.CreateGroup(ctx, &Group{ID: groupID, Name: "bench-group", OwnerID: userID})

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
		ID: userID, Email: "bench@test.com", PasswordHash: "h", DisplayName: "Bench",
	})
	groupID := uuid.New()
	_ = store.CreateGroup(ctx, &Group{ID: groupID, Name: "bench-group", OwnerID: userID})

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
