// Package faulttest provides fault-decorating implementations of the server's
// consumer ports — the session/device repositories, the agent-control seam, and
// the relay session registry — so tests can exercise the server's internal
// failure-handling behavior by substituting a faulting port around an in-process
// server.
//
// It is a test-support package. It is imported only from _test.go files and is
// never reachable from any production build; TestFaulttestNotShipped inspects
// the real dependency graph of cmd/meshserver and proves it excludes this
// package, which is the binding "no fault code in the shipped binary" guarantee.
// Fault selection is pure test wiring — there is no header, environment
// variable, or HTTP surface that could select a fault in a deployed server.
package faulttest

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Action is the kind of fault a Spec injects at a decorated call site.
type Action int

const (
	// ActionNone delegates to the real implementation unchanged.
	ActionNone Action = iota
	// ActionDelay waits (cancellable) before delegating to the real call.
	ActionDelay
	// ActionTimeout blocks until the call's context is canceled and returns the
	// context error — a dependency that never answers within the deadline.
	ActionTimeout
	// ActionError returns a typed error without invoking the real call.
	ActionError
	// ActionPanic panics, so the harness can assert middleware.Recoverer maps it
	// to 500 and the next request still succeeds.
	ActionPanic
	// ActionBlocked blocks until the call's context is canceled and returns the
	// context error — a hung dependency that only unblocks on cancellation.
	ActionBlocked
)

// ErrInjected is the default error ActionError returns and the default value
// ActionPanic panics with.
var ErrInjected = errors.New("faulttest: injected fault")

// Spec is an immutable description of a single fault. The zero value
// (ActionNone) delegates unchanged.
type Spec struct {
	// Action selects the fault behavior.
	Action Action
	// Delay is the cancellable wait for ActionDelay.
	Delay time.Duration
	// Err overrides ErrInjected for ActionError.
	Err error
	// PanicValue overrides ErrInjected for ActionPanic.
	PanicValue any
	// Once auto-clears the fault after it fires once, so the following call sees
	// the real implementation — used to prove a request survives a panic.
	Once bool
}

// errValue is the error ActionError returns.
func (s Spec) errValue() error {
	if s.Err != nil {
		return s.Err
	}
	return ErrInjected
}

// panicValue is the value ActionPanic panics with.
func (s Spec) panicValue() any {
	if s.PanicValue != nil {
		return s.PanicValue
	}
	return ErrInjected
}

// apply runs the fault. delegate reports whether the caller should proceed to
// the real implementation afterward; when delegate is false, err is returned to
// the caller. Every waiting action exits on ctx cancellation, so a faulted call
// can never outlive its request.
func (s Spec) apply(ctx context.Context) (delegate bool, err error) {
	switch s.Action {
	case ActionNone:
		return true, nil
	case ActionError:
		return false, s.errValue()
	case ActionPanic:
		panic(s.panicValue())
	case ActionDelay:
		timer := time.NewTimer(s.Delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-timer.C:
			return true, nil
		}
	case ActionTimeout, ActionBlocked:
		<-ctx.Done()
		return false, ctx.Err()
	default:
		return true, nil
	}
}

// faultSet holds the armed fault per method name for one decorator. It is safe
// for concurrent use: a test arms faults from its own goroutine while request
// goroutines read them.
type faultSet struct {
	mu     sync.Mutex
	byName map[string]Spec
}

func newFaultSet() *faultSet {
	return &faultSet{byName: make(map[string]Spec)}
}

func (fs *faultSet) arm(method string, s Spec) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.byName[method] = s
}

func (fs *faultSet) clear(method string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	delete(fs.byName, method)
}

// apply runs the fault armed for method, if any. A Once fault is cleared before
// it fires so a panic cannot leave it armed.
func (fs *faultSet) apply(ctx context.Context, method string) (delegate bool, err error) {
	fs.mu.Lock()
	s, ok := fs.byName[method]
	if ok && s.Once {
		delete(fs.byName, method)
	}
	fs.mu.Unlock()
	if !ok {
		return true, nil
	}
	return s.apply(ctx)
}
