package agentapi

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/alerts"
)

func TestGroupingComesFromTheRulesOwnDefinition(t *testing.T) {
	t.Parallel()
	f := alertConn(t)

	f.ingest(t, wellFormed(t))
	f.reachedStore(t, 1)

	shipped, ok := f.conn.ruleCatalog.Lookup("disk-critical")
	require.True(t, ok)
	got := f.store.groupedBy()
	require.Len(t, got, 1)
	assert.Equal(t, time.Duration(shipped.GroupWindowSecs)*time.Second, got[0].Window,
		"the hold is the rule's own, not a figure the ingest path chose")
	assert.Equal(t, alerts.ScopeDevice, got[0].Scope,
		"disk-critical is about a machine's volumes, so the room is about the machine")
}

func TestGroupingKeysThatAreNotRungsDoNotWidenTheRoom(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		groupBy []string
		want    alerts.Scope
	}{
		{"the machine and one of its volumes", []string{"device", "mount"}, alerts.ScopeDevice},
		{"an office", []string{"site"}, alerts.ScopeSite},
		{"a whole customer", []string{"organization"}, alerts.ScopeOrganization},
		{"the narrowest rung a rule names wins", []string{"organization", "device"}, alerts.ScopeDevice},
		{"a rule naming no rung is about the machine", []string{"metric"}, alerts.ScopeDevice},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, incidentScope(tc.groupBy))
		})
	}
}

func TestAnUnknownRuleGetsTheNarrowestRoom(t *testing.T) {
	t.Parallel()
	f := alertConn(t)
	f.conn.ruleCatalog = nil

	got := f.conn.groupingFor("a-rule-this-build-never-heard-of")

	assert.Equal(t, alerts.ScopeDevice, got.Scope)
	assert.Equal(t, fallbackGroupWindow, got.Window)
}
