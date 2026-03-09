package amt

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/mps"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	cm, err := cert.NewManager(dir)
	require.NoError(t, err)

	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	logger := discardLogger()
	mpsSrv := mps.NewServer(cm, store, logger)
	return NewService(mpsSrv, "admin", "password", logger)
}

func TestPowerActionDeviceNotConnected(t *testing.T) {
	svc := newTestService(t)
	err := svc.PowerAction(context.Background(), uuid.New(), 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestQueryDeviceInfoDeviceNotConnected(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.QueryDeviceInfo(context.Background(), uuid.New())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestConnectedDeviceCountDelegates(t *testing.T) {
	svc := newTestService(t)
	assert.Equal(t, 0, svc.ConnectedDeviceCount())
}
