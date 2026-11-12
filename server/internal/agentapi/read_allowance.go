package agentapi

import (
	"context"
	"math"

	"golang.org/x/sync/semaphore"
)

const (
	// maxMachineMessageBytes is the largest message a machine sends; a longer declared length is
	// refused from the header, before anything is allocated for it.
	maxMachineMessageBytes = 1 << 20
	// readAllowanceShare is the share of Go's memory limit every machine connection together may
	// hold read but not yet handled: each byte held costs four to six while it is decoded.
	readAllowanceShare = 0.05
	// unlimitedReadAllowanceBytes is the allowance of a server Go sets no memory limit for.
	unlimitedReadAllowanceBytes = 32 << 20
)

// readAllowanceFor is the read allowance under a Go memory limit, never below the largest message.
func readAllowanceFor(memoryLimit int64) int64 {
	if memoryLimit <= 0 || memoryLimit == math.MaxInt64 {
		return unlimitedReadAllowanceBytes
	}
	return max(int64(float64(memoryLimit)*readAllowanceShare), maxMachineMessageBytes)
}

// readAllowance bounds the bytes all connections hold between reading a message and handling it.
type readAllowance struct{ bytes *semaphore.Weighted }

func newReadAllowance(bytes int64) *readAllowance {
	return &readAllowance{bytes: semaphore.NewWeighted(bytes)}
}

// reserve waits until n bytes fit the allowance, or ctx ends, and returns how to give them back.
// A nil allowance reserves nothing.
func (r *readAllowance) reserve(ctx context.Context, n int64) (func(), error) {
	if r == nil || n == 0 {
		return func() {}, nil
	}
	if err := r.bytes.Acquire(ctx, n); err != nil {
		return nil, err
	}
	return func() { r.bytes.Release(n) }, nil
}
