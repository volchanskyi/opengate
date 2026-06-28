package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmailLimiter covers the per-email failed-login throttle: it trips after
// the configured number of failures regardless of source IP, normalizes the
// key, resets on success, and releases after the window expires.
func TestEmailLimiter(t *testing.T) {
	t.Parallel()

	t.Run("trips after max failures and is IP-independent", func(t *testing.T) {
		t.Parallel()
		l := newEmailLimiter(3, time.Minute)
		const email = "victim@example.com"
		for i := 0; i < 3; i++ {
			assert.True(t, l.allowed(email), "allowed before max reached")
			l.recordFailure(email)
		}
		assert.False(t, l.allowed(email), "locked after max failures")
	})

	t.Run("normalizes email so case and whitespace cannot bypass", func(t *testing.T) {
		t.Parallel()
		l := newEmailLimiter(2, time.Minute)
		l.recordFailure("User@Example.com")
		l.recordFailure("  user@example.com ")
		assert.False(t, l.allowed("USER@EXAMPLE.COM"), "variants must share one key")
	})

	t.Run("reset on success clears the counter", func(t *testing.T) {
		t.Parallel()
		l := newEmailLimiter(2, time.Minute)
		const email = "user@example.com"
		l.recordFailure(email)
		l.recordFailure(email)
		require.False(t, l.allowed(email))

		l.reset(email)
		assert.True(t, l.allowed(email), "success clears the lockout")
	})

	t.Run("window expiry releases the lock", func(t *testing.T) {
		t.Parallel()
		l := newEmailLimiter(1, time.Millisecond)
		const email = "user@example.com"
		l.recordFailure(email)
		require.False(t, l.allowed(email))

		time.Sleep(5 * time.Millisecond)
		assert.True(t, l.allowed(email), "lock expires after the window")
	})
}
