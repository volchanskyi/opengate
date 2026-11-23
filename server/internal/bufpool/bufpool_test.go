package bufpool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestABufferHoldsWhatWasAskedFor(t *testing.T) {
	t.Parallel()
	p := New(1 << 20)

	for _, n := range []int{0, 1, 511, 512, 513, 9000, 1 << 20} {
		b := p.Get(n)
		assert.Zero(t, len(b), "a buffer starts empty")
		assert.GreaterOrEqual(t, cap(b), n)
	}
}

func TestAReturnedBufferIsHandedOutAgain(t *testing.T) {
	t.Parallel()
	p := New(1 << 20)
	first := p.Get(9000)
	first = append(first, 'x')

	p.Put(first)
	again := p.Get(8500)

	require.NotZero(t, cap(again))
	assert.Same(t, &first[:1][0], &again[:1][0], "a buffer of the same class comes back")
	assert.Zero(t, len(again))
}

func TestAClassKeepsNoMoreThanItsBudget(t *testing.T) {
	t.Parallel()
	p := New(16 << 10)
	held := [][]byte{p.Get(8 << 10), p.Get(8 << 10), p.Get(8 << 10)}
	for _, b := range held {
		p.Put(b)
	}

	kept := 0
	for range 3 {
		b := p.Get(8 << 10)
		for _, h := range held {
			if &b[:1][0] == &h[:1][0] {
				kept++
			}
		}
	}
	assert.Equal(t, 2, kept, "two 8 KiB buffers fill a 16 KiB class; the third went to the collector")
}

func TestABufferOutsideTheClassesIsLeftToTheCollector(t *testing.T) {
	t.Parallel()
	p := New(64 << 20)
	huge := p.Get(2 << 20)
	tiny := make([]byte, 0, 100)

	p.Put(huge)
	p.Put(tiny)

	assert.NotSame(t, &huge[:1][0], &p.Get(2 << 20)[:1][0])
	assert.Equal(t, 512, cap(p.Get(100)), "the smallest class is half a kibibyte")
}

func TestAWarmCycleAllocatesNothing(t *testing.T) {
	p := New(1 << 20)
	p.Put(p.Get(64 << 10))

	allocs := testing.AllocsPerRun(100, func() { p.Put(p.Get(60 << 10)) })

	assert.Zero(t, allocs)
}
