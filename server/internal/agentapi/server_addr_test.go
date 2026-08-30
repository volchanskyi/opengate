package agentapi

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestAServerThatNeverListensStopsWaitingForItsAddress states the bound every
// caller of Addr depends on. Addr answers a question about a listener that may
// never come up — a TLS config that will not build, a port already taken — and
// nothing sends the address when it does not. Waiting for that send with no
// deadline hangs the whole test binary on a channel nobody will ever write, and
// a hang carries none of the information a failure does.
func TestAServerThatNeverListensStopsWaitingForItsAddress(t *testing.T) {
	t.Parallel()

	never := make(chan string)
	start := time.Now()
	addr, listening := waitForAddr(never, 50*time.Millisecond)

	assert.False(t, listening, "a listener that never came up is not listening")
	assert.Empty(t, addr, "there is no address to answer with")
	assert.Less(t, time.Since(start), 5*time.Second, "the wait is bounded, not indefinite")
}

// TestAListeningServerAnswersWithItsAddress is the other half: the bound costs
// a running listener nothing.
func TestAListeningServerAnswersWithItsAddress(t *testing.T) {
	t.Parallel()

	bound := make(chan string, 1)
	bound <- "127.0.0.1:4433"

	addr, listening := waitForAddr(bound, time.Minute)

	assert.True(t, listening)
	assert.Equal(t, "127.0.0.1:4433", addr)
}
