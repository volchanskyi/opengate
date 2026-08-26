package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// How a run gets the administrator session it needs before it can build
// anything: signing in where an account exists, and being the first account
// where none does.

func TestFixtureClientSignsInBeforeItBuilds(t *testing.T) {
	api := &fakeAPI{}
	client := newFixtureClient(t, api)

	require.NoError(t, client.SignIn("admin@service.invalid", "secret"))
	assert.True(t, api.loggedIn)
	assert.Equal(t, "operator-token", client.Token())
}

// An empty stack has no administrator to sign in as. The first account the
// server sees becomes one, which is how a fresh install bootstraps and how a
// throwaway stack gets its session.
func TestFixtureClientBootstrapsTheFirstAccountOnAnEmptyStack(t *testing.T) {
	api := &fakeAPI{}
	client := newFixtureClient(t, api)

	require.NoError(t, client.EnsureAdmin("first@service.invalid", "secret", true))
	assert.Equal(t, []string{"first@service.invalid"}, api.registered)
	assert.False(t, api.loggedIn, "an empty stack has nothing to sign in to")
	assert.Equal(t, "member-token", client.Token())
}

func TestFixtureClientSignsInWhereAnAccountAlreadyExists(t *testing.T) {
	api := &fakeAPI{}
	client := newFixtureClient(t, api)

	require.NoError(t, client.EnsureAdmin("admin@service.invalid", "secret", false))
	assert.True(t, api.loggedIn)
	assert.Empty(t, api.registered, "an environment that has an administrator gets no new account")
}

func TestFixtureClientReportsARefusedBootstrap(t *testing.T) {
	api := &fakeAPI{failAt: "/api/v1/auth/register"}
	client := newFixtureClient(t, api)

	err := client.EnsureAdmin("first@service.invalid", "secret", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sign in")
}

func TestFixtureClientReportsARefusedSignIn(t *testing.T) {
	api := &fakeAPI{failAt: "/api/v1/auth/login"}
	client := newFixtureClient(t, api)

	err := client.SignIn("admin@service.invalid", "secret")
	require.Error(t, err)
	// A run that cannot sign in builds nothing, so the message has to say that
	// rather than leaving every later call to fail on a missing token.
	assert.Contains(t, err.Error(), "sign in")
}
