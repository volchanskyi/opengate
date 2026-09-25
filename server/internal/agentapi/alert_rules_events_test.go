package agentapi

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
)

// The half of the pack the machine owns: which of those rules travel (none of
// them), what every rule that does travel has to carry, and how a customer
// stops one the machine is holding itself.

// A rule that watches the machine's own words is not sent to the machine.
//
// Its matching phrases are what the machine's log reader is built around, so
// they are already there; what the server holds for it is the rest of the rule.
// Sent down this path it would reach the number-comparing evaluator, which
// would report a rule it cannot evaluate — and every machine in the estate
// would then read as not watching a rule that is in fact watching all of them.
func TestARuleAboutTheMachinesOwnWordsIsNotSentToTheMachine(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	p := newCatalogueProvider(t, &fakeRuleConfig{}, nil)

	got, err := p.RulesFor(context.Background(), ladderFor(org))
	require.NoError(t, err)

	cat, err := rules.Embedded()
	require.NoError(t, err)

	sent := byRuleID(got.Rules)
	watchingWords := 0
	for _, def := range cat.All() {
		if !def.WatchesEvents() {
			assert.Contains(t, sent, def.ID, "%s watches a reading, so the machine needs it", def.ID)
			continue
		}
		watchingWords++
		assert.NotContains(t, sent, def.ID,
			"%s is already on the machine; sending it hands the wrong evaluator a rule it cannot read", def.ID)
	}
	require.Positive(t, watchingWords, "the shipped pack must contain rules about the machine's own words")
}

// Every rule that does reach a machine carries how bad it is, because the
// machine states that on each alert and a queue ordered by severity cannot
// order one that says nothing.
func TestEveryRuleReachingAMachineSaysHowBadItIs(t *testing.T) {
	t.Parallel()

	p := newCatalogueProvider(t, &fakeRuleConfig{}, nil)
	got, err := p.RulesFor(context.Background(), ladderFor(uuid.New()))
	require.NoError(t, err)
	require.NotEmpty(t, got.Rules)

	for _, rule := range got.Rules {
		assert.Truef(t, protocol.ValidAlertSeverity(rule.Severity),
			"%s reached a machine carrying severity %q", rule.ID, rule.Severity)
	}
}

// Stopping a rule that watches the machine's own words has to work too, and it
// cannot work the way stopping a rule about a reading does.
//
// A rule about a reading stops reaching the machine, so nothing is raised. A
// rule about words is compiled into the machine's log reader and goes on
// matching whatever the customer decided — so the ruleset carries which of them
// the customer still wants, and the alert path refuses the rest. Without this,
// the switch on Priya's screen would be a switch that does nothing.
func TestTheRulesetSaysWhichWordRulesTheCustomerStillWants(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	killed := rules.DefaultRollout(org, "linux-oom-kill")
	killed.Kill = true
	store := &fakeRuleConfig{rollouts: map[uuid.UUID]map[string]rules.Rollout{
		org: {"linux-oom-kill": killed},
	}}

	got, err := newCatalogueProvider(t, store, nil).RulesFor(context.Background(), ladderFor(org))
	require.NoError(t, err)

	assert.NotContains(t, got.EventRules, "linux-oom-kill",
		"a stopped rule is one whose alerts this customer no longer receives")
	assert.Contains(t, got.EventRules, "linux-hung-task",
		"and stopping one must not take the others with it")
}
