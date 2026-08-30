package transport

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestAnMPSListenerThatNeverBindsStopsBeingWaitedOn states the same bound the
// agent listener carries. Addr answers a question about a listener that may
// never come up, and nothing publishes an address when it does not — so a wait
// with no deadline stops the caller for good on a channel nobody will write.
func TestAnMPSListenerThatNeverBindsStopsBeingWaitedOn(t *testing.T) {
	t.Parallel()

	never := make(chan string)
	start := time.Now()
	addr, listening := waitForAddr(never, 50*time.Millisecond)

	assert.False(t, listening, "a listener that never came up is not listening")
	assert.Empty(t, addr, "there is no address to answer with")
	assert.Less(t, time.Since(start), 5*time.Second, "the wait is bounded, not indefinite")
}

// TestABoundMPSListenerAnswersWithItsAddress is the other half: the bound costs
// a running listener nothing.
func TestABoundMPSListenerAnswersWithItsAddress(t *testing.T) {
	t.Parallel()

	bound := make(chan string, 1)
	bound <- "127.0.0.1:4433"

	addr, listening := waitForAddr(bound, time.Minute)

	assert.True(t, listening)
	assert.Equal(t, "127.0.0.1:4433", addr)
}
