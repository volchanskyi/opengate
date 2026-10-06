package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goroutinePageAtStart = `goroutine profile: total 7
4 @ 0x43e5ce 0x44e5ed 0x6b0b4d 0x471b61
#	0x43e5cd	runtime.gopark+0x10d	/usr/local/go/src/runtime/proc.go:435
#	0x44e5ec	runtime.chanrecv+0x3ac	/usr/local/go/src/runtime/chan.go:664
#	0x6b0b4c	github.com/volchanskyi/opengate/server/internal/relay.(*Relay).pump+0x2c	/src/server/internal/relay/relay.go:118
#	0x471b60	runtime.goexit+0x0	/usr/local/go/src/runtime/asm_amd64.s:1700

2 @ 0x43e5ce 0x4a1111
#	0x4a1110	net/http.(*conn).serve+0x3f0	/usr/local/go/src/net/http/server.go:2092

1 @ 0x43e5ce
#	0x43e5cd	runtime.gopark+0x10d	/usr/local/go/src/runtime/proc.go:435
`

const goroutinePageAtEnd = `goroutine profile: total 31
28 @ 0x43e5ce 0x44e5ed 0x6b0b4d 0x471b61
#	0x43e5cd	runtime.gopark+0x10d	/usr/local/go/src/runtime/proc.go:435
#	0x44e5ec	runtime.chanrecv+0x3ac	/usr/local/go/src/runtime/chan.go:664
#	0x6b0b4c	github.com/volchanskyi/opengate/server/internal/relay.(*Relay).pump+0x2c	/src/server/internal/relay/relay.go:118
#	0x471b60	runtime.goexit+0x0	/usr/local/go/src/runtime/asm_amd64.s:1700

2 @ 0x43e5ce 0x4a1111
#	0x4a1110	net/http.(*conn).serve+0x3f0	/usr/local/go/src/net/http/server.go:2092

1 @ 0x43e5ce
#	0x43e5cd	runtime.gopark+0x10d	/usr/local/go/src/runtime/proc.go:435
`

const heapPageAtStart = `heap profile: 3: 1200 [5: 2400] @ heap/1048576
2: 1024 [2: 1024] @ 0x4a2b1d 0x471b61
#	0x4a2b1c	github.com/volchanskyi/opengate/server/internal/store.(*Store).remember+0x3c	/src/server/internal/store/store.go:42
#	0x471b60	runtime.goexit+0x0	/usr/local/go/src/runtime/asm_amd64.s:1700

1: 176 [3: 528] @ 0x4b0000
#	0x4affff	runtime.allocm+0x1f	/usr/local/go/src/runtime/proc.go:2000

# runtime.MemStats
# Alloc = 1200
# TotalAlloc = 2400
`

const heapPageAtEnd = `heap profile: 9: 9392 [11: 10592] @ heap/1048576
8: 9216 [8: 9216] @ 0x4a2b1d 0x471b61
#	0x4a2b1c	github.com/volchanskyi/opengate/server/internal/store.(*Store).remember+0x3c	/src/server/internal/store/store.go:42
#	0x471b60	runtime.goexit+0x0	/usr/local/go/src/runtime/asm_amd64.s:1700

1: 176 [3: 528] @ 0x4b0000
#	0x4affff	runtime.allocm+0x1f	/usr/local/go/src/runtime/proc.go:2000

# runtime.MemStats
# Alloc = 9392
`

func TestParseGoroutineStacksReadsCountsAndFrames(t *testing.T) {
	stacks, err := ParseGoroutineStacks(goroutinePageAtStart)
	require.NoError(t, err)
	require.Len(t, stacks, 3)

	assert.Equal(t, float64(4), stacks[0].Weight)
	require.Len(t, stacks[0].Frames, 4)
	assert.Equal(t, "runtime.gopark", stacks[0].Frames[0].Function)
	assert.Equal(t, "/usr/local/go/src/runtime/proc.go:435", stacks[0].Frames[0].Location)
	assert.Equal(t,
		"github.com/volchanskyi/opengate/server/internal/relay.(*Relay).pump",
		stacks[0].Frames[2].Function)
}

func TestParseHeapStacksWeighsBytesInUse(t *testing.T) {
	stacks, err := ParseHeapStacks(heapPageAtStart)
	require.NoError(t, err)
	require.Len(t, stacks, 2)

	assert.Equal(t, float64(1024), stacks[0].Weight)
	assert.Equal(t, float64(176), stacks[1].Weight)
}

func TestParsersRefuseAPageThatIsNotTheProfileTheyAskedFor(t *testing.T) {
	for name, page := range map[string]string{
		"an error page":     "404 page not found\n",
		"empty":             "",
		"the other profile": heapPageAtStart,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseGoroutineStacks(page)
			require.Error(t, err)
		})
	}

	_, err := ParseHeapStacks(goroutinePageAtStart)
	require.Error(t, err)
}

func TestSiteNamesTheFirstFrameThatIsNotStandardLibrary(t *testing.T) {
	stacks, err := ParseGoroutineStacks(goroutinePageAtStart)
	require.NoError(t, err)

	assert.Contains(t, stacks[0].Site(), "relay.(*Relay).pump")
	assert.Contains(t, stacks[0].Site(), "/src/server/internal/relay/relay.go:118")

	assert.Contains(t, stacks[2].Site(), "runtime.gopark")
}

func TestProductFrameTellsAModulePathFromTheStandardLibrary(t *testing.T) {
	for _, tc := range []struct {
		function string
		product  bool
	}{
		{"github.com/volchanskyi/opengate/server/internal/relay.(*Relay).pump", true},
		{"github.com/quic-go/quic-go.(*connection).run", true},
		{"main.main", true},
		{"runtime.gopark", false},
		{"net/http.(*conn).serve", false},
		{"sync.(*WaitGroup).Wait", false},
		{"internal/poll.(*FD).Read", false},
	} {
		assert.Equal(t, tc.product, Frame{Function: tc.function}.Product(), tc.function)
	}
}
