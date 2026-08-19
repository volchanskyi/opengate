package agentapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// How far a rule has been rolled out, and the stop that ends it — through the
// path a machine actually gets its rules on.

// countingFleet answers with a fixed estate size and records how often it was
// asked, so a case can prove the count is read for a customer mid-rollout and
// for nobody else.
type countingFleet struct {
	size  int
	calls int
	err   error
}

func (f *countingFleet) Counts(context.Context, uuid.UUID) (device.Counts, error) {
	f.calls++
	if f.err != nil {
		return device.Counts{}, f.err
	}
	return device.Counts{Total: f.size, Online: f.size}, nil
}

// stagedAt is one customer's rollout state for the disk rule at a given reach.
func stagedAt(org uuid.UUID, percent int) map[uuid.UUID]map[string]rules.Rollout {
	r := rules.DefaultRollout(org, "disk-critical")
	r.RolloutPercent = percent
	return map[uuid.UUID]map[string]rules.Rollout{org: {"disk-critical": r}}
}

// machinesWithTheDiskRule counts how many of an estate the staged rule reached,
// asking the provider once per machine exactly as a reconnect would.
func machinesWithTheDiskRule(t *testing.T, p *CatalogueAlertRuleProvider, org uuid.UUID, estate int) int {
	t.Helper()
	count := 0
	for i := range estate {
		scope := settings.Scope{
			DeviceID:       uuid.NewSHA1(uuid.Nil, fmt.Appendf(nil, "device-%d", i)),
			OrganizationID: org,
			TenantID:       uuid.New(),
		}
		if _, ok := mustResolve(t, p, scope)["disk-critical"]; ok {
			count++
		}
	}
	return count
}

// A rule being tried reaches the machines its stage covers and no others. The
// whole mitigation for a bad curated rule is that the first hour of it costs a
// handful of endpoints rather than the estate.
func TestStagedRuleReachesOnlyItsStage(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	const estate = 200
	fleet := &countingFleet{size: estate}
	p := newRolloutProvider(t, &fakeRuleConfig{rollouts: stagedAt(org, 1)}, fleet)

	got := machinesWithTheDiskRule(t, p, org, estate)
	want := rules.StagePopulation(1, estate)
	assert.InDelta(t, want, got, float64(want)/2+3,
		"a canary aims at %d of %d machines and reached %d", want, estate, got)
	assert.Less(t, got, estate, "a canary is not the estate")
}

// The rest of the pack is untouched by one rule being staged: a machine outside
// the canary is still watched by everything else.
func TestStagingOneRuleLeavesTheRestOfThePack(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	p := newRolloutProvider(t, &fakeRuleConfig{rollouts: stagedAt(org, 1)}, &countingFleet{size: 2000})

	outside := settings.Scope{
		DeviceID:       uuid.NewSHA1(uuid.Nil, []byte("a machine outside the canary")),
		OrganizationID: org,
		TenantID:       uuid.New(),
	}
	got := mustResolve(t, p, outside)
	assert.NotContains(t, got, "disk-critical", "this machine is not in the canary")
	assert.Contains(t, got, "cpu-saturated", "and is still watched by the rest of the pack")
}

// Sizing a stage costs a count of the customer's estate, so it is read for the
// customers who are mid-rollout and for nobody else — which is every customer,
// on every reconnect, until somebody stages something.
func TestTheEstateIsCountedOnlyForACustomerMidRollout(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	scope := ladderFor(org)

	full := &countingFleet{size: 2000}
	assert.Contains(t, mustResolve(t, newRolloutProvider(t, &fakeRuleConfig{}, full), scope), "disk-critical")
	assert.Zero(t, full.calls, "a customer who has staged nothing needs no count")

	partial := &countingFleet{size: 2000}
	mustResolve(t, newRolloutProvider(t, &fakeRuleConfig{rollouts: stagedAt(org, 10)}, partial), scope)
	assert.Equal(t, 1, partial.calls, "a customer mid-rollout is what the count is for")
}

// An estate that cannot be counted loses the canary floor and nothing else. The
// staged rule reaches the share it declares — never the estate, which is what
// guessing upward on a failed count would do.
func TestAnUncountableEstateStillGetsItsRules(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	const estate = 2000
	fleet := &countingFleet{size: estate, err: errors.New("database is down")}
	p := newRolloutProvider(t, &fakeRuleConfig{rollouts: stagedAt(org, 1)}, fleet)

	got := machinesWithTheDiskRule(t, p, org, estate)
	assert.Positive(t, got, "the rule still reaches the machines it names")
	assert.Less(t, got, estate/10, "and must not spread to the estate because a count failed")

	outside := settings.Scope{DeviceID: uuid.New(), OrganizationID: org, TenantID: uuid.New()}
	assert.Contains(t, mustResolve(t, p, outside), "cpu-saturated",
		"a failed count costs the stage its floor, not the machine its rules")
}

// A provider with no way to count an estate behaves the same way: the stage is
// sized from the share alone rather than the rollout being abandoned.
func TestAProviderWithoutAFleetSourceStillStages(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	p := NewCatalogueAlertRuleProvider(mustCatalogue(t), &fakeRuleConfig{rollouts: stagedAt(org, 1)}, nil, nil, nil, testLogger())
	assert.Less(t, machinesWithTheDiskRule(t, p, org, 500), 500/10)
}

// The kill switch, proven on the path that makes it need no deploy: an agent
// that was offline when the switch was flipped stops the rule as it reconnects,
// because reconnecting re-resolves what it should be running.
func TestReconnectStopsAKilledRuleWithoutADeploy(t *testing.T) {
	t.Parallel()

	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := testutil.SeedSite(t, ctx, store)
	machine := testutil.SeedDevice(t, ctx, store, site.ID)

	config := &fakeRuleConfig{}
	ac, buf := newRuleConn(t, store, machine.ID, site.ID, config)

	// It connects and is given the whole pack.
	require.NoError(t, ac.handleRegister(ctx, ruleRegisterMsg()))
	assert.Contains(t, pushedRules(t, ac, buf), "disk-critical")

	// The rule starts degrading machines and is killed while this one is
	// offline. No deploy, no restart — one row.
	killed := rules.DefaultRollout(site.OrganizationID, "disk-critical")
	killed.Kill = true
	config.rollouts = map[uuid.UUID]map[string]rules.Rollout{
		site.OrganizationID: {"disk-critical": killed},
	}

	buf.Reset()
	require.NoError(t, ac.handleRegister(ctx, ruleRegisterMsg()))
	got := pushedRules(t, ac, buf)
	assert.NotContains(t, got, "disk-critical", "a reconnecting agent stops the killed rule")
	assert.Contains(t, got, "cpu-saturated", "and keeps everything that was not killed")
}

// The other half of "whichever is sooner": a machine that is already connected
// stops the rule at the next push of its ruleset, without waiting to reconnect.
func TestAConnectedAgentStopsAKilledRuleAtTheNextPush(t *testing.T) {
	t.Parallel()

	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := testutil.SeedSite(t, ctx, store)
	machine := testutil.SeedDevice(t, ctx, store, site.ID)

	killed := rules.DefaultRollout(site.OrganizationID, "disk-critical")
	killed.Kill = true
	config := &fakeRuleConfig{rollouts: map[uuid.UUID]map[string]rules.Rollout{
		site.OrganizationID: {"disk-critical": killed},
	}}
	ac, buf := newRuleConn(t, store, machine.ID, site.ID, config)

	require.NoError(t, ac.pushAlertRules(ctx))
	got := pushedRules(t, ac, buf)
	assert.NotContains(t, got, "disk-critical")
	assert.Contains(t, got, "cpu-saturated")
}

// mustCatalogue is the shipped pack, which every case here resolves against.
func mustCatalogue(t *testing.T) *rules.Catalogue {
	t.Helper()
	cat, err := rules.Embedded()
	require.NoError(t, err)
	return cat
}

func newRolloutProvider(t *testing.T, store RuleConfigStore, fleet FleetCounter) *CatalogueAlertRuleProvider {
	t.Helper()
	return NewCatalogueAlertRuleProvider(mustCatalogue(t), store, nil, fleet, nil, testLogger())
}

// newRuleConn is a connection carrying everything the rule push reads: the
// machine's own place in the tenancy ladder, read from the database exactly as a
// live connection reads it.
func newRuleConn(t *testing.T, store *db.PostgresStore, deviceID, siteID uuid.UUID, config RuleConfigStore) (*AgentConn, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	return &AgentConn{
		DeviceID:     deviceID,
		SiteID:       siteID,
		TenantID:     dbtx.DefaultTenantID,
		Capabilities: []protocol.AgentCapability{protocol.CapThresholdAlerts},
		stream:       buf,
		codec:        &protocol.Codec{},
		devices:      testutil.NewTestDevices(t, store),
		settings:     settings.NewPostgresReader(store.DB()),
		alertRules:   newRolloutProvider(t, config, nil),
		logger:       testLogger(),
	}, buf
}

// ruleRegisterMsg registers an agent that evaluates threshold rules and collects
// no inventory, so the ruleset is the only thing the push writes.
func ruleRegisterMsg() *protocol.ControlMessage {
	return &protocol.ControlMessage{
		Type:         protocol.MsgAgentRegister,
		Capabilities: []protocol.AgentCapability{protocol.CapThresholdAlerts},
		Hostname:     "dal-ws-012",
		OS:           "linux",
		Arch:         "amd64",
		Version:      "1.2.3",
	}
}

// pushedRules reads the ruleset the server just wrote to the agent.
func pushedRules(t *testing.T, ac *AgentConn, buf *bytes.Buffer) map[string]protocol.ThresholdRule {
	t.Helper()
	msg := readOutboundControl(t, ac, buf)
	require.Equal(t, protocol.MsgPushAlertRules, msg.Type)
	return byRuleID(msg.AlertRules)
}
