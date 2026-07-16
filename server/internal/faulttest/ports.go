package faulttest

import (
	"context"

	"github.com/volchanskyi/opengate/server/internal/api"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/session"
)

// Each decorator embeds the real port so unfaulted methods delegate unchanged,
// and overrides the faultable methods to consult the armed fault first. Every
// override threads the caller's context to both the fault executor and the real
// call, so the tenant scope (dbtx GUC) always survives.

// FaultDevices decorates a device.Repository.
type FaultDevices struct {
	device.Repository
	faults *faultSet
}

// WrapDevices returns a device.Repository decorator around real.
func WrapDevices(real device.Repository) *FaultDevices {
	return &FaultDevices{Repository: real, faults: newFaultSet()}
}

// Arm sets the fault for a method name (e.g. "Get", "List"). Clear removes it.
func (f *FaultDevices) Arm(method string, s Spec) { f.faults.arm(method, s) }

// Clear removes the fault armed for method.
func (f *FaultDevices) Clear(method string) { f.faults.clear(method) }

// Get consults the "Get" fault, then delegates.
func (f *FaultDevices) Get(ctx context.Context, id device.DeviceID) (*device.Device, error) {
	if delegate, err := f.faults.apply(ctx, "Get"); !delegate {
		return nil, err
	}
	return f.Repository.Get(ctx, id)
}

// List consults the "List" fault, then delegates.
func (f *FaultDevices) List(ctx context.Context, groupID device.GroupID) ([]*device.Device, error) {
	if delegate, err := f.faults.apply(ctx, "List"); !delegate {
		return nil, err
	}
	return f.Repository.List(ctx, groupID)
}

// FaultSessions decorates a session.Repository.
type FaultSessions struct {
	session.Repository
	faults *faultSet
}

// WrapSessions returns a session.Repository decorator around real.
func WrapSessions(real session.Repository) *FaultSessions {
	return &FaultSessions{Repository: real, faults: newFaultSet()}
}

// Arm sets the fault for a method name.
func (f *FaultSessions) Arm(method string, s Spec) { f.faults.arm(method, s) }

// Clear removes the fault armed for method.
func (f *FaultSessions) Clear(method string) { f.faults.clear(method) }

// Create consults the "Create" fault, then delegates.
func (f *FaultSessions) Create(ctx context.Context, s *session.Session) error {
	if delegate, err := f.faults.apply(ctx, "Create"); !delegate {
		return err
	}
	return f.Repository.Create(ctx, s)
}

// Get consults the "Get" fault, then delegates.
func (f *FaultSessions) Get(ctx context.Context, token string) (*session.Session, error) {
	if delegate, err := f.faults.apply(ctx, "Get"); !delegate {
		return nil, err
	}
	return f.Repository.Get(ctx, token)
}

// FaultRegistry decorates a relay.SessionRegistry, injected via
// relay.WithRegistry — ServerConfig.Relay is a concrete *relay.Relay, so the
// registry interface is the seam, not a *relay.Relay decorator.
type FaultRegistry struct {
	relay.SessionRegistry
	faults *faultSet
}

// WrapRegistry returns a relay.SessionRegistry decorator around real.
func WrapRegistry(real relay.SessionRegistry) *FaultRegistry {
	return &FaultRegistry{SessionRegistry: real, faults: newFaultSet()}
}

// Arm sets the fault for a method name.
func (f *FaultRegistry) Arm(method string, s Spec) { f.faults.arm(method, s) }

// Clear removes the fault armed for method.
func (f *FaultRegistry) Clear(method string) { f.faults.clear(method) }

// SaveSession consults the "SaveSession" fault, then delegates.
func (f *FaultRegistry) SaveSession(ctx context.Context, token protocol.SessionToken, meta relay.SessionMeta) error {
	if delegate, err := f.faults.apply(ctx, "SaveSession"); !delegate {
		return err
	}
	return f.SessionRegistry.SaveSession(ctx, token, meta)
}

// Ping consults the "Ping" fault, then delegates — the readiness-probe seam.
func (f *FaultRegistry) Ping(ctx context.Context) error {
	if delegate, err := f.faults.apply(ctx, "Ping"); !delegate {
		return err
	}
	return f.SessionRegistry.Ping(ctx)
}

// FaultAgentControl decorates the FI0 api.AgentControl seam. Connection-close is
// performed by the harness on the concrete connection it owns; there is no
// Close on this port.
type FaultAgentControl struct {
	api.AgentControl
	faults *faultSet
}

// WrapAgentControl returns an api.AgentControl decorator around real.
func WrapAgentControl(real api.AgentControl) *FaultAgentControl {
	return &FaultAgentControl{AgentControl: real, faults: newFaultSet()}
}

// Arm sets the fault for a method name.
func (f *FaultAgentControl) Arm(method string, s Spec) { f.faults.arm(method, s) }

// Clear removes the fault armed for method.
func (f *FaultAgentControl) Clear(method string) { f.faults.clear(method) }

// SendSessionRequest consults the "SendSessionRequest" fault, then delegates.
func (f *FaultAgentControl) SendSessionRequest(ctx context.Context, token protocol.SessionToken, relayURL string, perms protocol.Permissions) error {
	if delegate, err := f.faults.apply(ctx, "SendSessionRequest"); !delegate {
		return err
	}
	return f.AgentControl.SendSessionRequest(ctx, token, relayURL, perms)
}

// Compile-time assertions that each decorator still satisfies its port.
var (
	_ device.Repository     = (*FaultDevices)(nil)
	_ session.Repository    = (*FaultSessions)(nil)
	_ relay.SessionRegistry = (*FaultRegistry)(nil)
	_ api.AgentControl      = (*FaultAgentControl)(nil)
)
