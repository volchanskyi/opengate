package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

func TestIceServersToAPI(t *testing.T) {
	t.Run("with credentials", func(t *testing.T) {
		servers := []signaling.ICEServer{
			{URLs: []string{"stun:stun.example.com"}, Username: "user1", Credential: "pass1"},
		}
		result := iceServersToAPI(servers)
		assert.Len(t, result, 1)
		assert.Equal(t, []string{"stun:stun.example.com"}, result[0].Urls)
		assert.NotNil(t, result[0].Username)
		assert.Equal(t, "user1", *result[0].Username)
		assert.NotNil(t, result[0].Credential)
		assert.Equal(t, "pass1", *result[0].Credential)
	})

	t.Run("without credentials", func(t *testing.T) {
		servers := []signaling.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		}
		result := iceServersToAPI(servers)
		assert.Len(t, result, 1)
		assert.Nil(t, result[0].Username)
		assert.Nil(t, result[0].Credential)
	})

	t.Run("empty slice", func(t *testing.T) {
		result := iceServersToAPI([]signaling.ICEServer{})
		assert.Empty(t, result)
	})

	t.Run("multiple servers", func(t *testing.T) {
		servers := []signaling.ICEServer{
			{URLs: []string{"stun:stun1.example.com"}},
			{URLs: []string{"turn:turn1.example.com"}, Username: "u", Credential: "c"},
		}
		result := iceServersToAPI(servers)
		assert.Len(t, result, 2)
		assert.Nil(t, result[0].Username)
		assert.NotNil(t, result[1].Username)
	})
}

func TestDerefStr(t *testing.T) {
	t.Run("non-nil", func(t *testing.T) {
		s := "hello"
		assert.Equal(t, "hello", derefStr(&s))
	})

	t.Run("nil", func(t *testing.T) {
		assert.Equal(t, "", derefStr[string](nil))
	})
}

func TestDerefInt(t *testing.T) {
	t.Run("non-nil", func(t *testing.T) {
		v := 42
		assert.Equal(t, 42, derefInt(&v, 0))
	})

	t.Run("nil uses fallback", func(t *testing.T) {
		assert.Equal(t, 10, derefInt(nil, 10))
	})
}

func TestDerefBool(t *testing.T) {
	t.Run("non-nil true", func(t *testing.T) {
		v := true
		assert.True(t, derefBool(&v))
	})

	t.Run("non-nil false", func(t *testing.T) {
		v := false
		assert.False(t, derefBool(&v))
	})

	t.Run("nil", func(t *testing.T) {
		assert.False(t, derefBool(nil))
	})
}

func TestDeviceToAPI(t *testing.T) {
	now := time.Now().UTC()
	d := &db.Device{
		ID:           uuid.New(),
		GroupID:      uuid.New(),
		Hostname:     "test-host",
		OS:           "linux",
		OsDisplay:    "Ubuntu 22.04",
		AgentVersion: "0.1.0",
		Capabilities: []string{"Terminal", "FileManager"},
		Status:       db.StatusOnline,
		LastSeen:     now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	result := deviceToAPI(d)
	assert.Equal(t, d.ID, result.Id)
	assert.Equal(t, d.Hostname, result.Hostname)
	assert.NotNil(t, result.OsDisplay)
	assert.Equal(t, "Ubuntu 22.04", *result.OsDisplay)

	t.Run("empty os display", func(t *testing.T) {
		d2 := &db.Device{OS: "linux"}
		result2 := deviceToAPI(d2)
		assert.Nil(t, result2.OsDisplay)
	})
}

func TestPermissionsToProtocol(t *testing.T) {
	t.Run("nil defaults to all true", func(t *testing.T) {
		p := permissionsToProtocol(nil)
		assert.True(t, p.Desktop)
		assert.True(t, p.Terminal)
		assert.True(t, p.FileRead)
		assert.True(t, p.FileWrite)
		assert.True(t, p.Input)
	})

	t.Run("explicit values", func(t *testing.T) {
		tr := true
		fa := false
		p := permissionsToProtocol(&Permissions{
			Desktop:  &tr,
			Terminal: &fa,
		})
		assert.True(t, p.Desktop)
		assert.False(t, p.Terminal)
		assert.False(t, p.FileRead)
	})
}

func TestManifestToAPI(t *testing.T) {
	m := &updater.Manifest{
		Version:   "1.0.0",
		OS:        "linux",
		Arch:      "amd64",
		URL:       "https://example.com/agent",
		SHA256:    "abc123",
		Signature: "sig456",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	result := manifestToAPI(m)
	assert.Equal(t, m.Version, result.Version)
	assert.Equal(t, m.OS, result.Os)
	assert.Equal(t, m.URL, result.Url)
	assert.Equal(t, m.SHA256, result.Sha256)
	assert.Equal(t, m.Signature, result.Signature)
}

func TestDeviceLogsToAPI(t *testing.T) {
	entries := []db.DeviceLogEntry{
		{Timestamp: "2026-01-01T00:00:00Z", Level: "INFO", Target: "agent", Message: "started"},
		{Timestamp: "2026-01-01T00:01:00Z", Level: "WARN", Target: "agent", Message: "slow"},
	}

	t.Run("has more", func(t *testing.T) {
		result := deviceLogsToAPI(entries, 10, db.LogFilter{Offset: 0, Limit: 2})
		assert.Len(t, result.Entries, 2)
		assert.Equal(t, 10, result.Total)
		assert.True(t, result.HasMore)
	})

	t.Run("no more", func(t *testing.T) {
		result := deviceLogsToAPI(entries, 2, db.LogFilter{Offset: 0, Limit: 5})
		assert.False(t, result.HasMore)
	})
}

func TestMapSlice(t *testing.T) {
	input := []int{1, 2, 3}
	result := mapSlice(input, func(i int) string {
		return string(rune('a' + i - 1))
	})
	assert.Equal(t, []string{"a", "b", "c"}, result)
}
