package agentapi

import (
	"context"
	"fmt"
	"time"

	"github.com/volchanskyi/opengate/server/internal/device"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/osutil"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// handleRegister completes enrollment and records the server-side duration, which spans the
// device row write and bringing the device online.
func (a *AgentConn) handleRegister(ctx context.Context, msg *protocol.ControlMessage) error {
	start := time.Now()
	err := a.register(ctx, msg)
	if a.metrics != nil {
		result := appmetrics.RegistrationOK
		if err != nil {
			result = appmetrics.RegistrationError
		}
		a.metrics.ObserveAgentRegistration(result, time.Since(start))
	}
	return err
}

func (a *AgentConn) register(ctx context.Context, msg *protocol.ControlMessage) error {
	osName := osutil.NormalizeOS(msg.OS)
	arch := osutil.NormalizeArch(msg.Arch)
	a.setMeta(osName, arch, msg.Version, msg.Capabilities)

	caps := make([]string, len(msg.Capabilities))
	for i, c := range msg.Capabilities {
		caps[i] = string(c)
	}

	d := &device.Device{
		ID:           a.DeviceID,
		SiteID:       a.SiteID,
		Hostname:     msg.Hostname,
		OS:           osName,
		OsDisplay:    msg.OS,
		AgentVersion: msg.Version,
		Capabilities: caps,
		Status:       device.StatusOnline,
	}

	if err := a.devices.Upsert(ctx, d); err != nil {
		return fmt.Errorf("upsert device: %w", err)
	}

	if err := a.devices.SetStatus(ctx, a.DeviceID, device.StatusOnline); err != nil {
		return fmt.Errorf("set device online: %w", err)
	}

	a.logger.Info("agent registered",
		"device_id", a.DeviceID,
		"hostname", msg.Hostname,
		"os", msg.OS,
		"capabilities", msg.Capabilities,
	)

	// A capability error means the agent did not opt in; no push error fails registration.
	if err := a.pushAlertRules(ctx); err != nil && !IsCapabilityError(err) {
		a.logger.Warn("push alert rules failed", "device_id", a.DeviceID, "error", err)
	}

	// Agents start Active on every registration, so only a device in maintenance needs a push.
	a.pushMaintenanceState(ctx)

	if err := a.SendRequestHardwareReport(ctx); err != nil && !IsCapabilityError(err) {
		a.logger.Warn("request hardware report on register failed", "device_id", a.DeviceID, "error", err)
	}

	return nil
}
