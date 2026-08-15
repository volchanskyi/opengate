package agentapi

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/alerts"
)

// How wide a room is and how long firings stay one thing are the rule's to say,
// not the ingest path's. What follows pins where each answer is read from, and
// what happens when the rule cannot be resolved at all.
// TestGroupingComesFromTheRulesOwnDefinition pins where a room's shape is
// decided. The rule says what its alerts are about and how long two firings stay
// one thing; anything the ingest path decided for itself would put a rule's
// alerts in a room its author never described.
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

// TestGroupingKeysThatAreNotRungsDoNotWidenTheRoom is why the shipped disk rule
// above lands at device scope despite naming two keys. A rule may group on the
// mount or the metric as well as the machine, but those say which volume or
// dimension a firing was about — a property of the alert, not of the room. A
// server with a full data volume and a full system volume has two alerts and one
// room to visit it in.
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

// TestAnUnknownRuleGetsTheNarrowestRoom keeps a wiring gap from merging
// customers. A connection with no catalogue cannot say how a rule groups, and
// the two ways of guessing are not equally bad: too wide puts two customers'
// unrelated events in one room, while too narrow only ever produces more rooms
// than necessary.
func TestAnUnknownRuleGetsTheNarrowestRoom(t *testing.T) {
	t.Parallel()
	f := alertConn(t)
	f.conn.ruleCatalog = nil

	got := f.conn.groupingFor("a-rule-this-build-never-heard-of")

	assert.Equal(t, alerts.ScopeDevice, got.Scope)
	assert.Equal(t, fallbackGroupWindow, got.Window)
}
