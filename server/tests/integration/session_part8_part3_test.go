package integration

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"net/http"
	"testing"
)

func TestSessionLifecycleDeleteNonexistentSession(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)

	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	// Try to delete a session that doesn't exist → 404
	status := env.deleteSession(t, jwtToken, "nonexistent-token-that-does-not-exist-at-all-1234567890abcdef")
	assert.Equal(t, http.StatusNotFound, status)

	_ = ctx // used for seeding
}
