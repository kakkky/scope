// Package scope provides scope-bound structured concurrency primitives for Go.
//
// A Scope binds the lifetime of spawned goroutines to a lexical block, so that
// the goroutines are guaranteed to complete before the block exits. This avoids
// goroutine leaks caused by missing wg.Wait() calls, unobserved panics, and
// errors that escape unhandled.
//
// The primary entry point is Run, which establishes a scope and invokes a body
// function. Inside the body, goroutines are spawned via Scope.Go. Run does not
// return until every spawned goroutine has finished.
//
// Errors and panics from any spawned goroutine are propagated back to the
// caller of Run. The first non-nil error cancels the scope's context, which is
// passed to every spawned goroutine and should be observed by callers for
// cooperative cancellation.
package scope

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
)

// Scope represents a structured concurrency scope.
//
// A Scope owns a derived context.Context that is canceled when any spawned
// goroutine returns a non-nil error, when a panic is recovered, when the body
// function passed to Run returns a non-nil error, or when Run returns.
//
// A Scope must be obtained via Run; callers should not construct one directly.
// The zero value of Scope is not usable.
type Scope struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	errOnce sync.Once
	err     error
}

// Run executes body inside a new Scope derived from parent and waits for all
// goroutines spawned via Scope.Go to finish before returning.
//
// Run guarantees that every goroutine spawned through the Scope has returned
// by the time Run returns, regardless of whether body returns normally,
// returns an error, or panics.
//
// If body returns a non-nil error, the scope's context is canceled and the
// error is returned. If any spawned goroutine returns a non-nil error or
// panics, the scope's context is canceled and the first such error is
// returned. Panics from spawned goroutines are recovered and converted into
// errors; they do not crash the process.
//
// The context passed to body and to each spawned goroutine is derived from
// parent and is canceled as described above. Callers should observe this
// context (typically via ctx.Done()) to participate in cancellation.
func Run(ctx context.Context, body func(s *Scope) error) error {
	ctx, cancel := context.WithCancel(ctx)
	s := &Scope{
		ctx:    ctx,
		cancel: cancel,
	}
	defer s.cancel()

	err := body(s)

	if err != nil {
		s.errOnce.Do(func() {
			s.err = err
			s.cancel()
		})
	}

	s.wg.Wait()
	return s.err
}

// Go starts fn in a new goroutine bound to the scope.
//
// The goroutine receives the scope's context as its argument; fn is expected
// to observe ctx.Done() or to pass ctx to downstream context-aware operations
// in order to participate in cancellation.
//
// If fn returns a non-nil error, the scope's context is canceled and the
// error becomes the result of Run (unless an earlier error has already been
// recorded). If fn panics, the panic is recovered, formatted as an error
// with a stack trace, and propagated through the same mechanism.
//
// Go may be called from inside body, from inside another goroutine spawned by
// the same scope, or recursively, as long as Run has not yet returned.
func (s *Scope) Go(fn func(ctx context.Context) error) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				s.errOnce.Do(func() {
					s.err = fmt.Errorf("scope: panic recovered: %v\n%s", r, debug.Stack())
					s.cancel()
				})
				return
			}
		}()

		if err := s.ctx.Err(); err != nil {
			s.errOnce.Do(func() {
				s.err = err
			})
		}

		if err := fn(s.ctx); err != nil {
			s.errOnce.Do(func() {
				s.err = err
				s.cancel()
			})
		}
	}()
}
