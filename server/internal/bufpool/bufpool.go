// Package bufpool keeps freed byte buffers for reuse in power-of-two size classes.
package bufpool

import (
	"math/bits"
	"sync"
)

const (
	// smallestClass and largestClass are the class sizes as powers of two: 512 B to 1 MiB.
	smallestClass = 9
	largestClass  = 20
	classes       = largestClass - smallestClass + 1
)

// Pool holds freed buffers, each size class at most its budget of bytes. A mutex guards it, so
// what it hands back is the same with and without the race detector.
type Pool struct {
	mu     sync.Mutex
	budget int
	idle   [classes][][]byte
	held   [classes]int
}

// New returns a pool keeping at most budget bytes of buffers in each class.
func New(budget int) *Pool {
	return &Pool{budget: budget}
}

// Get returns an empty buffer whose capacity holds n bytes, a freed one when its class has one.
// A size past the largest class is allocated and never kept.
func (p *Pool) Get(n int) []byte {
	class, ok := classFor(n)
	if !ok {
		return make([]byte, 0, n)
	}
	p.mu.Lock()
	idle := p.idle[class]
	if last := len(idle) - 1; last >= 0 {
		b := idle[last]
		idle[last] = nil
		p.idle[class] = idle[:last]
		p.held[class] -= cap(b)
		p.mu.Unlock()
		return b[:0]
	}
	p.mu.Unlock()
	return make([]byte, 0, 1<<(class+smallestClass))
}

// Put keeps b for a later Get of its class while the class is under budget. The caller hands over
// b with every slice of it, since the next Get writes over it.
func (p *Pool) Put(b []byte) {
	size := cap(b)
	if size < 1<<smallestClass || size > 1<<largestClass {
		return
	}
	class := bits.Len(uint(size)) - 1 - smallestClass
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.held[class]+size > p.budget {
		return
	}
	p.idle[class] = append(p.idle[class], b[:0])
	p.held[class] += size
}

// classFor is the smallest class holding n bytes, false past the largest.
func classFor(n int) (int, bool) {
	if n > 1<<largestClass {
		return 0, false
	}
	if n <= 1<<smallestClass {
		return 0, true
	}
	return bits.Len(uint(n-1)) - smallestClass, true
}
