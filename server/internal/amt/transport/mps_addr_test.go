package transport

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAnMPSListenerThatNeverBindsStopsBeingWaitedOn(t *testing.T) {
	t.Parallel()

	never := make(chan string)
	start := time.Now()
	addr, listening := waitForAddr(never, 50*time.Millisecond)

	assert.False(t, listening, "a listener that never came up is not listening")
	assert.Empty(t, addr, "there is no address to answer with")
	assert.Less(t, time.Since(start), 5*time.Second, "the wait is bounded, not indefinite")
}

func TestABoundMPSListenerAnswersWithItsAddress(t *testing.T) {
	t.Parallel()

	bound := make(chan string, 1)
	bound <- "127.0.0.1:4433"

	addr, listening := waitForAddr(bound, time.Minute)

	assert.True(t, listening)
	assert.Equal(t, "127.0.0.1:4433", addr)
}
