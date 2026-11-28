package agentapi

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

const (
	// machineOpeningWindow is what a machine may send before the server reads it, so a whole fleet
	// sending at once parks at most this much each in the transport.
	machineOpeningWindow = 16 << 10
	// receiveBudgetShare is the share of Go's memory limit every machine connection together may
	// grow its window by beyond the opening one.
	receiveBudgetShare = 0.05
	// unlimitedReceiveBudgetBytes is the budget of a server Go sets no memory limit for.
	unlimitedReceiveBudgetBytes = 32 << 20
)

// receiveBudgetFor is the growth budget under a Go memory limit, never below one window grown to
// the largest message.
func receiveBudgetFor(memoryLimit int64) int64 {
	if memoryLimit <= 0 || memoryLimit == math.MaxInt64 {
		return unlimitedReceiveBudgetBytes
	}
	return max(int64(float64(memoryLimit)*receiveBudgetShare), maxMachineMessageBytes)
}

// receiveBudget is the window growth every machine connection draws on; a connection keeps what
// it was granted until it closes.
type receiveBudget struct {
	mu      sync.Mutex
	left    int64
	granted map[*quic.Conn]int64
}

func newReceiveBudget(bytes int64) *receiveBudget {
	return &receiveBudget{left: bytes, granted: map[*quic.Conn]int64{}}
}

// allow grants conn delta more window when the budget holds it. The transport calls it with the
// connection's lock held, so it touches nothing of the connection but its address.
func (b *receiveBudget) allow(conn *quic.Conn, delta uint64) bool {
	if delta > math.MaxInt64 {
		return false
	}
	n := int64(delta)
	b.mu.Lock()
	defer b.mu.Unlock()
	if n > b.left {
		return false
	}
	b.left -= n
	b.granted[conn] += n
	return true
}

// release hands back everything conn was granted.
func (b *receiveBudget) release(conn *quic.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.left += b.granted[conn]
	delete(b.granted, conn)
}

// watch releases conn's share once the connection closes.
func (b *receiveBudget) watch(conn *quic.Conn) {
	context.AfterFunc(conn.Context(), func() { b.release(conn) })
}

// machineQUICConfig is the machine-facing transport: each connection opens with a small window and
// grows it only from budget.
func machineQUICConfig(budget *receiveBudget) *quic.Config {
	return &quic.Config{
		MaxIdleTimeout:                 90 * time.Second,
		KeepAlivePeriod:                30 * time.Second,
		InitialStreamReceiveWindow:     machineOpeningWindow,
		InitialConnectionReceiveWindow: machineOpeningWindow,
		MaxStreamReceiveWindow:         maxMachineMessageBytes,
		MaxConnectionReceiveWindow:     maxMachineMessageBytes,
		AllowConnectionWindowIncrease:  budget.allow,
	}
}
