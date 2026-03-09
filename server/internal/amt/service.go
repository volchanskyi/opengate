// Package amt coordinates AMT device operations by combining the MPS
// connection layer with WSMAN tunneling.
package amt

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/mps"
	"github.com/volchanskyi/opengate/server/internal/mps/wsman"
)

// ErrDeviceNotConnected is returned when an operation targets a device
// that has no active CIRA connection.
var ErrDeviceNotConnected = errors.New("device not connected")

// Service provides high-level AMT device operations.
type Service struct {
	mps      *mps.Server
	username string
	password string
	logger   *slog.Logger
}

// NewService creates an AMT service that uses the given MPS server for connections.
func NewService(mpsSrv *mps.Server, username, password string, logger *slog.Logger) *Service {
	return &Service{
		mps:      mpsSrv,
		username: username,
		password: password,
		logger:   logger,
	}
}

// PowerAction sends a power command to a connected AMT device.
func (s *Service) PowerAction(ctx context.Context, amtUUID uuid.UUID, state int) error {
	conn := s.mps.GetConn(amtUUID)
	if conn == nil {
		return ErrDeviceNotConnected
	}
	client := wsman.NewClient(conn, s.username, s.password, s.logger)
	return client.RequestPowerStateChange(ctx, wsman.PowerState(state))
}

// QueryDeviceInfo queries a connected AMT device for its info.
func (s *Service) QueryDeviceInfo(ctx context.Context, amtUUID uuid.UUID) (*wsman.DeviceInfo, error) {
	conn := s.mps.GetConn(amtUUID)
	if conn == nil {
		return nil, ErrDeviceNotConnected
	}
	client := wsman.NewClient(conn, s.username, s.password, s.logger)
	return client.GetDeviceInfo(ctx)
}

// ConnectedDeviceCount returns the number of active AMT connections.
func (s *Service) ConnectedDeviceCount() int {
	return s.mps.ConnectedDeviceCount()
}
