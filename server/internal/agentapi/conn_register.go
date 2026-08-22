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

// Registration is the one moment a whole fleet does the same thing at once — a
// rollout, a site coming back after an outage, a reconnect storm — so how long
// it takes and how often it fails is the number that says whether the server
// absorbed it. It is measured here, where the device row lands, because that is
// the only place the operation has actually happened.

// handleRegister completes enrollment and reports how long the server took
// over it. The clock covers the whole operation — the device row written and
// the device brought online — because that is when the agent is actually
// registered. A client timing its own send cannot see any of it: handing the
// register frame to a QUIC stream returns as soon as the bytes are buffered
// locally, which is why enrollment latency is a server-side measurement.
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

	// Deliver the agent's tenant-scoped threshold-alert ruleset (WS-19). A
	// capability error just means the agent did not opt in; only a real send
	// failure is worth logging, and neither fails registration.
	if err := a.pushAlertRules(ctx); err != nil && !IsCapabilityError(err) {
		a.logger.Warn("push alert rules failed", "device_id", a.DeviceID, "error", err)
	}

	// Reconcile a suppressed device: agents default to Active on every fresh
	// registration, so only a device currently in maintenance needs a push to
	// re-suppress a reconnecting agent. Exiting maintenance is delivered by the
	// toggle handler's unconditional push, not here.
	a.pushMaintenanceState(ctx)

	// Refresh the stored inventory: a device that reconnects may have rebooted
	// with different RAM, disks or interfaces, so coming back online is what
	// keeps the hardware card current. A capability error just means the agent
	// does not collect an inventory; neither case fails registration.
	if err := a.SendRequestHardwareReport(ctx); err != nil && !IsCapabilityError(err) {
		a.logger.Warn("request hardware report on register failed", "device_id", a.DeviceID, "error", err)
	}

	return nil
}
