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

// countingFleet answers with a fixed estate size and records how often it was asked.
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

// machinesWithTheDiskRule counts the machines of an estate that receive the disk rule.
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

const (
	// A tenth of this estate is well above the canary floor and a hundredth is below it.
	rolloutEstate = 200
	canaryPercent = 1
	stagedPercent = 10
	// The fewest machines a partial stage runs on.
	canaryFloorMachines = 5
	// Stage membership is hashed per machine, so the realised count varies around its target.
	canarySlack = 3.0
	stagedSlack = 6.0
)

func TestStagedRuleReachesOnlyItsStage(t *testing.T) {
	t.Parallel()

	org := uuid.New()

	canary := newRolloutProvider(t,
		&fakeRuleConfig{rollouts: stagedAt(org, canaryPercent)}, &countingFleet{size: rolloutEstate})
	got := machinesWithTheDiskRule(t, canary, org, rolloutEstate)
	assert.InDelta(t, canaryFloorMachines, got, canarySlack,
		"a canary aims at the %d-machine floor of %d and reached %d",
		canaryFloorMachines, rolloutEstate, got)

	staged := newRolloutProvider(t,
		&fakeRuleConfig{rollouts: stagedAt(org, stagedPercent)}, &countingFleet{size: rolloutEstate})
	gotStaged := machinesWithTheDiskRule(t, staged, org, rolloutEstate)
	assert.InDelta(t, rolloutEstate*stagedPercent/100, gotStaged, stagedSlack,
		"a staged rule aims at a tenth of %d machines and reached %d", rolloutEstate, gotStaged)
	assert.Greater(t, gotStaged, got, "the staged step reaches more machines than the canary")
	assert.Less(t, gotStaged, rolloutEstate, "a staged rule is not the estate")

	full := newRolloutProvider(t,
		&fakeRuleConfig{rollouts: stagedAt(org, 100)}, &countingFleet{size: rolloutEstate})
	assert.Equal(t, rolloutEstate, machinesWithTheDiskRule(t, full, org, rolloutEstate),
		"a rule at full reach is on every machine")
}

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

func TestAProviderWithoutAFleetSourceStillStages(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	p := NewCatalogueAlertRuleProvider(mustCatalogue(t), &fakeRuleConfig{rollouts: stagedAt(org, 1)}, nil, nil, nil, testLogger())
	assert.Less(t, machinesWithTheDiskRule(t, p, org, 500), 500/10)
}

func TestReconnectStopsAKilledRuleWithoutADeploy(t *testing.T) {
	t.Parallel()

	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := testutil.SeedSite(t, ctx, store)
	machine := testutil.SeedDevice(t, ctx, store, site.ID)

	config := &fakeRuleConfig{}
	ac, buf := newRuleConn(t, store, machine.ID, site.ID, config)

	require.NoError(t, ac.handleRegister(ctx, ruleRegisterMsg()))
	assert.Contains(t, pushedRules(t, ac, buf), "disk-critical")

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

// mustCatalogue returns the shipped rule catalogue.
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

// newRuleConn builds a connection whose tenancy ladder is read from the database.
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

// ruleRegisterMsg registers an agent that evaluates threshold rules and collects no inventory.
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

func TestARuleChangeReachesMachinesAlreadyConnected(t *testing.T) {
	t.Parallel()

	contoso, fabrikam := uuid.New(), uuid.New()
	srv, machines := connectedFleet(t, map[uuid.UUID]int{contoso: 3, fabrikam: 2})

	reached := srv.RefreshAlertRules(context.Background(), contoso)

	assert.Equal(t, 3, reached, "every connected machine of that customer is reached")
	for _, m := range machines[contoso] {
		assert.Truef(t, m.received(protocol.MsgPushAlertRules),
			"%s must be given the ruleset as it now stands", m.name)
	}
	for _, m := range machines[fabrikam] {
		assert.Falsef(t, m.received(protocol.MsgPushAlertRules),
			"%s belongs to another customer and must be left alone", m.name)
	}
}

func TestARuleChangeWithNobodyConnectedReachesNobody(t *testing.T) {
	t.Parallel()

	srv, _ := connectedFleet(t, map[uuid.UUID]int{uuid.New(): 2})

	assert.Zero(t, srv.RefreshAlertRules(context.Background(), uuid.New()),
		"a customer with no machines on the wire is reached by nobody, and that is not a failure")
}

func TestATenantWideChangeReachesEveryMachineInTheTenant(t *testing.T) {
	t.Parallel()

	contoso, fabrikam := uuid.New(), uuid.New()
	srv, machines := connectedFleet(t, map[uuid.UUID]int{contoso: 2, fabrikam: 2})

	reached := srv.RefreshAlertRulesForTenant(context.Background(), theTenant)

	assert.Equal(t, 4, reached, "both customers' machines are in one tenant")
	for _, fleet := range machines {
		for _, m := range fleet {
			assert.Truef(t, m.received(protocol.MsgPushAlertRules), "%s must be reached", m.name)
		}
	}

	assert.Zero(t, srv.RefreshAlertRulesForTenant(context.Background(), uuid.New()),
		"and a stop in one tenant reaches nobody in another")
}

func TestOneUnreachableMachineDoesNotStopTheOthers(t *testing.T) {
	t.Parallel()

	contoso := uuid.New()
	srv, machines := connectedFleet(t, map[uuid.UUID]int{contoso: 3})
	machines[contoso][0].fail = true

	reached := srv.RefreshAlertRules(context.Background(), contoso)

	assert.Equal(t, 2, reached, "the two that could be written to were")
	assert.False(t, machines[contoso][0].received(protocol.MsgPushAlertRules))
}

// theTenant is the tenant every machine in the refresh cases belongs to.
var theTenant = uuid.New()

// wiredMachine is a connection registered with the server plus the frames written to it.
type wiredMachine struct {
	name string
	conn *AgentConn
	out  *refusingWriter
	fail bool
}

// received reports whether the machine was written one of these.
func (m *wiredMachine) received(msgType protocol.ControlMessageType) bool {
	codec := &protocol.Codec{}
	reader := bytes.NewReader(m.out.written.Bytes())
	for {
		frame, payload, err := codec.ReadFrame(reader)
		if err != nil {
			return false
		}
		if frame != protocol.FrameControl {
			continue
		}
		msg, err := codec.DecodeControl(payload)
		if err != nil {
			return false
		}
		if msg.Type == msgType {
			return true
		}
	}
}

// refusingWriter is a machine's stream that a case can break.
type refusingWriter struct {
	written bytes.Buffer
	machine *wiredMachine
}

func (w *refusingWriter) Write(p []byte) (int, error) {
	if w.machine != nil && w.machine.fail {
		return 0, errors.New("the link to this machine broke")
	}
	return w.written.Write(p)
}

func (w *refusingWriter) Read([]byte) (int, error) { return 0, errors.New("nothing to read") }
func (w *refusingWriter) Close() error             { return nil }

// connectedFleet brings up a server holding one connection per machine across the named customers,
// all in one tenant.
func connectedFleet(t *testing.T, estates map[uuid.UUID]int) (*AgentServer, map[uuid.UUID][]*wiredMachine) {
	t.Helper()

	srv := &AgentServer{logger: testLogger()}
	fleet := make(map[uuid.UUID][]*wiredMachine, len(estates))

	for org, size := range estates {
		for i := range size {
			deviceID := uuid.New()
			machine := &wiredMachine{name: fmt.Sprintf("machine-%d-of-%s", i, org)}
			machine.out = &refusingWriter{machine: machine}
			machine.conn = &AgentConn{
				DeviceID:     deviceID,
				TenantID:     theTenant,
				Capabilities: []protocol.AgentCapability{protocol.CapThresholdAlerts},
				stream:       machine.out,
				codec:        &protocol.Codec{},
				settings: fixedReader{scope: settings.Scope{
					DeviceID:       deviceID,
					OrganizationID: org,
					TenantID:       theTenant,
				}},
				alertRules: newRolloutProvider(t, &fakeRuleConfig{}, nil),
				logger:     testLogger(),
			}
			require.NoError(t, machine.conn.pushAlertRules(context.Background()))
			machine.out.written.Reset()

			srv.conns.Store(deviceID, machine.conn)
			fleet[org] = append(fleet[org], machine)
		}
	}
	return srv, fleet
}
