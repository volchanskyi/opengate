package agentapi

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// recordingProvider captures the ladder it was asked to answer for.
type recordingProvider struct {
	seen settings.Scope
}

func (p *recordingProvider) RulesFor(scope settings.Scope) []protocol.ThresholdRule {
	p.seen = scope
	return DefaultAlertRules()
}

// fixedReader is a settings.Reader that always answers with one ladder, or one
// error.
type fixedReader struct {
	scope settings.Scope
	err   error
}

func (r fixedReader) ScopeFor(context.Context, uuid.UUID) (settings.Scope, error) {
	return r.scope, r.err
}

// TestAlertRulesCarryTheMachineCustomer closes the trap the tenancy plan lists:
// alerts and vitals arrive on the agent connection, so the customer a machine
// belongs to has to be derivable there. The rule push is where that walk happens.
func TestAlertRulesCarryTheMachineCustomer(t *testing.T) {
	deviceID := uuid.New()
	ladder := settings.Scope{
		DeviceID:       deviceID,
		SiteID:         uuid.New(),
		OrganizationID: uuid.New(),
		TenantID:       uuid.New(),
	}
	provider := &recordingProvider{}

	ac := &AgentConn{
		DeviceID:     deviceID,
		TenantID:     ladder.TenantID,
		codec:        &protocol.Codec{},
		logger:       testLogger(),
		alertRules:   provider,
		settings:     fixedReader{scope: ladder},
		Capabilities: []protocol.AgentCapability{protocol.CapThresholdAlerts},
	}
	var buf bytes.Buffer
	ac.stream = &buf

	require.NoError(t, ac.pushAlertRules(context.Background()))
	assert.Equal(t, ladder, provider.seen, "the whole ladder reaches the provider, not just the tenant")
	assert.NotEqual(t, uuid.Nil, provider.seen.OrganizationID, "the customer is derivable on the connection")
}

// TestAlertRulesFallBackToWhatTheConnectionKnows covers the read failing. The
// tenant boundary is what must hold, and the connection already knows its own,
// so a failed walk loses the narrower targeting rather than the wall.
func TestAlertRulesFallBackToWhatTheConnectionKnows(t *testing.T) {
	deviceID := uuid.New()
	tenantID := uuid.New()
	siteID := uuid.New()
	provider := &recordingProvider{}

	ac := &AgentConn{
		DeviceID:     deviceID,
		TenantID:     tenantID,
		SiteID:       siteID,
		codec:        &protocol.Codec{},
		logger:       testLogger(),
		alertRules:   provider,
		settings:     fixedReader{err: errors.New("database unavailable")},
		Capabilities: []protocol.AgentCapability{protocol.CapThresholdAlerts},
	}
	var buf bytes.Buffer
	ac.stream = &buf

	require.NoError(t, ac.pushAlertRules(context.Background()))
	assert.Equal(t, tenantID, provider.seen.TenantID, "the tenant the connection authenticated as still holds")
	assert.Equal(t, deviceID, provider.seen.DeviceID)
	assert.Equal(t, uuid.Nil, provider.seen.OrganizationID, "and the customer is simply unknown, not guessed")
}

// TestAlertRulesWithNoReaderUseWhatTheConnectionKnows covers the optional
// dependency being absent, which is how every test-time connection is built.
func TestAlertRulesWithNoReaderUseWhatTheConnectionKnows(t *testing.T) {
	tenantID := uuid.New()
	provider := &recordingProvider{}

	ac := &AgentConn{
		DeviceID:     uuid.New(),
		TenantID:     tenantID,
		codec:        &protocol.Codec{},
		logger:       testLogger(),
		alertRules:   provider,
		Capabilities: []protocol.AgentCapability{protocol.CapThresholdAlerts},
	}
	var buf bytes.Buffer
	ac.stream = &buf

	require.NoError(t, ac.pushAlertRules(context.Background()))
	assert.Equal(t, tenantID, provider.seen.TenantID)
}
