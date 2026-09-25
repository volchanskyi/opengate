package acceptance

import (
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// Reading what the product pushed to a machine.
//
// A case about something arriving asks a different question from one about
// something arriving *again*, and conflating the two passes against whatever
// registration delivered rather than against the change under test.
//
// The mark is taken before the change, never after. The product delivers a rule
// change while the request that made it is still open, so a count read
// afterwards has already included the push it was meant to wait for — and the
// wait then sits there for one that is never coming. It passes only while the
// machine is slower than the request, which is a test that passes for a reason
// that has nothing to do with the product.

// PushesSoFar is how many messages of this type the product has already sent,
// taken before the change under test.
func (m *Machine) PushesSoFar(msgType protocol.ControlMessageType) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := 0
	for _, msg := range m.inbox {
		if msg.Type == msgType {
			seen++
		}
	}
	return seen
}

// AwaitPast waits for a message of this type beyond `mark`, and returns the
// newest one the machine has been pushed.
func (m *Machine) AwaitPast(msgType protocol.ControlMessageType, mark int) *protocol.ControlMessage {
	m.t.Helper()

	var newest *protocol.ControlMessage
	require.Eventuallyf(m.t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		seen := 0
		for _, msg := range m.inbox {
			if msg.Type != msgType {
				continue
			}
			seen++
			newest = msg
		}
		return seen > mark
	}, eventually, poll, "the product never pushed another %s to the machine", msgType)
	return newest
}
