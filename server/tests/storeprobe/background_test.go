package main

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheBackgroundWritesProductionsSeriesEachInterval(t *testing.T) {
	store := &countingStore{}
	server := httptest.NewServer(store)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	require.NoError(t, runBackground(ctx, backgroundConfig{store: server.URL, series: 300, every: 100 * time.Millisecond}))

	writes := store.lines.Load() / 300
	assert.GreaterOrEqual(t, writes, int64(2), "the whole series set lands each interval")
	assert.Zero(t, store.lines.Load()%300, "an interval writes every series once")
}

func TestABackgroundIntervalNamesEachSeriesOnce(t *testing.T) {
	body := backgroundBody(250, time.Unix(1_700_000_000, 0))
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	require.Len(t, lines, 250)
	seen := map[string]bool{}
	for _, line := range lines {
		series := line[:strings.LastIndex(line[:strings.LastIndex(line, " ")], " ")]
		assert.False(t, seen[series])
		seen[series] = true
	}
	assert.True(t, bytes.HasSuffix(body, []byte(" 1700000000000\n")), "stamped with the interval's time")
}
