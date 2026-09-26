package agentapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A connection that has no catalogue cannot tell a rule about the machine's own
// words from one about a reading, so it filters nothing out: a stop it cannot
// read is enforced on the machine's next reconnect instead of by dropping
// alerts here.
func TestAConnectionWithNoCatalogueTreatsEveryRuleAsWanted(t *testing.T) {
	t.Parallel()

	conn := &AgentConn{wantedEventRules: map[string]struct{}{}}
	assert.True(t, conn.customerWants("linux-oom-kill"))
}
