package agentapi

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
)

// changedMidSignIn is Contoso's provider with a tuning change landing right after a sign-in's
// ruleset is built, before it reaches the machine.
type changedMidSignIn struct {
	*CatalogueAlertRuleProvider
	c       twoCustomers
	changed atomic.Bool
}

func (p *changedMidSignIn) RulesForMachine(ctx context.Context, machine rules.Device) (RuleSet, error) {
	set, err := p.CatalogueAlertRuleProvider.RulesForMachine(ctx, machine)
	if err == nil && p.changed.CompareAndSwap(false, true) {
		p.c.store.bindings = bindingsFor(p.c.contoso.OrganizationID, diskBinding(p.c.contoso.OrganizationID, 85))
		err = p.CatalogueAlertRuleProvider.Reload(p.c.contosoCtx, p.c.contoso.OrganizationID)
	}
	return set, err
}

// alwaysChanging is a provider whose customer settings move every time they are asked about.
type alwaysChanging struct {
	*CatalogueAlertRuleProvider
	stamp atomic.Uint64
}

func (p *alwaysChanging) Version(organizationID uuid.UUID) uint64 {
	return p.CatalogueAlertRuleProvider.Version(organizationID) + p.stamp.Add(1)
}

func signInRuleConn(provider AlertRuleProvider, ref CustomerRef) (*AgentConn, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return &AgentConn{
		DeviceID:     uuid.New(),
		TenantID:     ref.TenantID,
		Capabilities: []protocol.AgentCapability{protocol.CapThresholdAlerts},
		stream:       buf,
		codec:        &protocol.Codec{},
		alertRules:   provider,
		logger:       testLogger(),
	}, buf
}

// pushedDiskThresholds reads every ruleset written to the machine, in order, as its disk threshold.
func pushedDiskThresholds(t *testing.T, ac *AgentConn, buf *bytes.Buffer) []float64 {
	t.Helper()
	var got []float64
	for buf.Len() > 0 {
		msg := readOutboundControl(t, ac, buf)
		require.Equal(t, protocol.MsgPushAlertRules, msg.Type)
		got = append(got, byRuleID(msg.AlertRules)["disk-critical"].Threshold)
	}
	return got
}

func TestASignInThatRacedATuningChangeSendsTheChangedRules(t *testing.T) {
	c := newTwoCustomers(t)
	require.NoError(t, c.provider.Load(context.Background(), directory{c.contoso, c.fabrikam}))
	provider := &changedMidSignIn{CatalogueAlertRuleProvider: c.provider, c: c}
	ac, buf := signInRuleConn(provider, c.contoso)

	require.NoError(t, ac.pushSignInRules(c.contosoCtx, c.machineOf(c.contoso)))

	assert.Equal(t, []float64{70, 85}, pushedDiskThresholds(t, ac, buf),
		"the machine ends on the tuning stored while it signed in")
}

func TestASignInWithSettingsAtRestSendsItsRulesOnce(t *testing.T) {
	c := newTwoCustomers(t)
	require.NoError(t, c.provider.Load(context.Background(), directory{c.contoso, c.fabrikam}))
	ac, buf := signInRuleConn(c.provider, c.contoso)

	require.NoError(t, ac.pushSignInRules(c.contosoCtx, c.machineOf(c.contoso)))

	assert.Equal(t, []float64{70}, pushedDiskThresholds(t, ac, buf))
}

func TestASignInBehindSettingsThatKeepMovingStopsSending(t *testing.T) {
	c := newTwoCustomers(t)
	require.NoError(t, c.provider.Load(context.Background(), directory{c.contoso, c.fabrikam}))
	ac, buf := signInRuleConn(&alwaysChanging{CatalogueAlertRuleProvider: c.provider}, c.contoso)

	require.NoError(t, ac.pushSignInRules(c.contosoCtx, c.machineOf(c.contoso)))

	assert.Len(t, pushedDiskThresholds(t, ac, buf), maxSignInRulePushes)
}
