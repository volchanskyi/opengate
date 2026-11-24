package agentapi

import (
	"context"
	"fmt"
	"time"

	"github.com/volchanskyi/opengate/server/internal/device"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/osutil"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// handleRegister answers an AgentRegister on a connection already signed in with a sign-in of
// its own, the connection already held; a failed one ends the connection.
func (a *AgentConn) handleRegister(ctx context.Context, msg *protocol.ControlMessage) error {
	signedIn, err := a.signIn(ctx, msg, time.Now(), nil)
	if err != nil {
		return err
	}
	a.afterSignIn(ctx, msg, signedIn)
	return nil
}

// signIn writes the machine online, handing held what it read before the write shows, and records
// the time from the AgentRegister's arrival at since, a wait in the line included.
func (a *AgentConn) signIn(ctx context.Context, msg *protocol.ControlMessage, since time.Time,
	held func(device.SignIn),
) (device.SignIn, error) {
	signedIn, err := a.writeSignIn(ctx, msg, held)
	if a.metrics != nil {
		result := appmetrics.RegistrationOK
		if err != nil {
			result = appmetrics.RegistrationError
		}
		a.metrics.ObserveAgentRegistration(result, time.Since(since))
	}
	return signedIn, err
}

func (a *AgentConn) writeSignIn(ctx context.Context, msg *protocol.ControlMessage,
	held func(device.SignIn),
) (device.SignIn, error) {
	osName := osutil.NormalizeOS(msg.OS)
	arch := osutil.NormalizeArch(msg.Arch)
	a.setMeta(osName, arch, msg.Version, msg.Capabilities)

	caps := make([]string, len(msg.Capabilities))
	for i, c := range msg.Capabilities {
		caps[i] = string(c)
	}

	// The row is what makes a machine exist for the product, so a failed sign-in ends it.
	signedIn, err := a.devices.SignIn(ctx, &device.Device{
		ID:           a.DeviceID,
		Hostname:     msg.Hostname,
		OS:           osName,
		OsDisplay:    msg.OS,
		AgentVersion: msg.Version,
		Capabilities: caps,
		Status:       device.StatusOnline,
	}, a.needsFleetSize, held)
	if err != nil {
		return device.SignIn{}, fmt.Errorf("sign in device: %w", err)
	}
	return signedIn, nil
}

// needsFleetSize asks the rule provider whether the customer's rollouts are sized by its count.
func (a *AgentConn) needsFleetSize(organizationID device.OrganizationID) bool {
	return a.alertRules != nil && a.alertRules.NeedsFleetSize(organizationID)
}

// afterSignIn sends what a signed-in machine is owed, from what its sign-in read.
func (a *AgentConn) afterSignIn(ctx context.Context, msg *protocol.ControlMessage, signedIn device.SignIn) {
	a.logger.Info("agent registered",
		"device_id", a.DeviceID,
		"hostname", msg.Hostname,
		"os", msg.OS,
		"capabilities", msg.Capabilities,
	)

	machine := rules.Device{
		Scope: settings.Scope{
			DeviceID:       a.DeviceID,
			SiteID:         signedIn.SiteID,
			OrganizationID: signedIn.OrganizationID,
			TenantID:       signedIn.TenantID,
		},
		Tags:      signedIn.Tags,
		FleetSize: signedIn.FleetSize,
	}
	// A capability error means the agent did not opt in; no push error fails registration.
	if err := a.pushSignInRules(ctx, machine); err != nil && !IsCapabilityError(err) {
		a.logger.Warn("push alert rules failed", "device_id", a.DeviceID, "error", err)
	}

	// Agents start Active on every registration, so only a device in maintenance needs a push.
	a.pushMaintenanceState(ctx, signedIn.MaintenanceOn)

	if err := a.SendRequestHardwareReport(ctx); err != nil && !IsCapabilityError(err) {
		a.logger.Warn("request hardware report on register failed", "device_id", a.DeviceID, "error", err)
	}

	// The bundle follows the deny-list check and the device row, so its arrival is the machine's
	// proof that the server accepted its identity.
	if len(a.trustBundle) > 0 {
		if err := a.SendTrustBundle(ctx, string(a.trustBundle)); err != nil {
			a.logger.Warn("send trust bundle", "device_id", a.DeviceID, "error", err)
		}
	}
	a.offerRenewal(ctx)
	if a.registered != nil {
		a.registered(a)
	}
}
