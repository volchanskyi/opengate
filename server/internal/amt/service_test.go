package amt

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/mps"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	store := testutil.NewTestStore(t)

	cm, err := cert.NewManager(t.TempDir())
	assert.NoError(t, err)

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
