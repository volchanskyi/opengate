package agentapi

import (
	"context"

	"golang.org/x/sync/semaphore"
)

const (
	// maxMachineMessageBytes is the largest message a machine sends; a longer declared length is
	// refused from the header, before anything is allocated for it.
	maxMachineMessageBytes = 1 << 20
	// readAllowanceBytes is what every machine connection together may hold read but not yet
	// handled; a reader past it waits.
	readAllowanceBytes = 32 << 20
)

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
