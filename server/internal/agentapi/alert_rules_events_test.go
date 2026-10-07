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
