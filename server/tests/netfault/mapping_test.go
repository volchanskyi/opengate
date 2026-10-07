package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMappingExpiryOutlivesTheLongestOutage(t *testing.T) {
	t.Parallel()
	const longestDarkWindow = 180 * time.Second
	assert.Greater(t, mappingIdleExpiry, longestDarkWindow,
		"a mapping expires inside the drill's own outage")
	assert.Equal(t, 600*time.Second, mappingIdleExpiry)
}

func TestMappingsExpireOnlyAfterIdling(t *testing.T) {
	t.Parallel()
	table := newMappingTable(time.Minute)
	at := base

	table.touch("10.0.0.1:1000", at)
	table.touch("10.0.0.2:1000", at)
	assert.Empty(t, table.expired(at.Add(59*time.Second)), "a mapping expired before its idle window")

	table.touch("10.0.0.1:1000", at.Add(30*time.Second))
	assert.Equal(t, []string{"10.0.0.2:1000"}, table.expired(at.Add(61*time.Second)),
		"the wrong mappings expired")
}

func TestQuietMachinesReleaseTheirMapping(t *testing.T) {
	t.Parallel()
	server := newEchoServer(t)
	shaper, err := NewShaper(Config{
		Listen:     "127.0.0.1:0",
		ServerAddr: server.addr().String(),
		Seed:       1,
		// Far below the drill's own window so the reaper can be watched.
		IdleExpiry: 10 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(shaper.Close)
	go shaper.Serve()

	conn := machine(t, shaper.ListenAddr())
	_, err = exchange(t, conn, "once")
	require.NoError(t, err)
	require.Equal(t, 1, shaper.Machines())

	assert.Eventually(t, func() bool { return shaper.Machines() == 0 },
		2*reapInterval+readDeadline, 100*time.Millisecond,
		"a machine that stopped talking kept its server-facing socket")
}

func TestTheExpiryTableForgetsWhatWasReleased(t *testing.T) {
	t.Parallel()
	table := newMappingTable(time.Minute)
	table.touch("10.0.0.1:1000", base)
	require.Equal(t, []string{"10.0.0.1:1000"}, table.expired(base.Add(2*time.Minute)))

	table.forget("10.0.0.1:1000")
	assert.Empty(t, table.expired(base.Add(2*time.Minute)),
		"a released machine was still named as expiring")
}

func TestAnUnstatedIdleWindowIsTheDrillsOwn(t *testing.T) {
	t.Parallel()
	server := newEchoServer(t)
	shaper, err := NewShaper(Config{Listen: "127.0.0.1:0", ServerAddr: server.addr().String()})
	require.NoError(t, err)
	t.Cleanup(shaper.Close)
	assert.Equal(t, mappingIdleExpiry, shaper.idleExpiry)
}
