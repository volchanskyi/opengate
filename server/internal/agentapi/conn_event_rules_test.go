package agentapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAConnectionWithNoCatalogueTreatsEveryRuleAsWanted(t *testing.T) {
	t.Parallel()

	conn := &AgentConn{wantedEventRules: map[string]struct{}{}}
	assert.True(t, conn.customerWants("linux-oom-kill"))
}
