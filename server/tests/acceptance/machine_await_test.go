package acceptance

import (
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// PushesSoFar counts the messages of this type already pushed, so a mark can be taken before
// the change that triggers a push; the product pushes while the request is still open.
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

// AwaitPast waits for a message of this type beyond mark and returns the newest one.
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
