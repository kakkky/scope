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

	cond    *sync.Cond
	activeG int
	closed  bool

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
//
// The opts parameter is reserved for future use; currently no options are
// defined and any provided values are ignored.
func Run(ctx context.Context, body func(s *Scope) error, opts ...Option) error {
	ctx, cancel := context.WithCancel(ctx)
	s := &Scope{
		ctx:    ctx,
		cancel: cancel,
		cond:   sync.NewCond(&sync.Mutex{}),
	}
	defer s.cancel()

	err := body(s)

	if err != nil {
		s.errOnce.Do(func() {
			s.err = err
			s.cancel()
		})
	}

	s.cond.L.Lock()
	for s.activeG > 0 {
		s.cond.Wait()
	}
	s.closed = true
	s.cond.L.Unlock()

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
//
// Calling Go after Run has returned panics. Goroutines must be spawned while
// Run is executing.
func (s *Scope) Go(fn func(ctx context.Context) error) {
	s.cond.L.Lock()
	if s.closed {
		s.cond.L.Unlock()
		panic("scope: misuse: Go called outside scope lifetime")
	}
	s.activeG++
	s.cond.L.Unlock()

	go func() {
		defer func() {
			s.cond.L.Lock()
			s.activeG--
			if s.activeG == 0 {
				s.cond.Signal()
			}
			s.cond.L.Unlock()
		}()
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

// Scope creates a child scope nested under s and runs body within it.
//
// Scope is synchronous: it does not return until body has returned and every
// goroutine spawned in the child scope has finished. The child scope's
// context is derived from s's context, so cancellation of s propagates to
// the child and to all goroutines spawned within it.
//
// Errors and panics originating inside the child scope (from body itself or
// from any goroutine spawned via the child's Go) are propagated to s through
// the same mechanism as Scope.Go: the first such error becomes the result of
// the enclosing Run (unless an earlier error has already been recorded), and
// s's context is canceled so sibling goroutines spawned via s.Go can observe
// cancellation.
//
// Scope makes the nested structure of a scope tree explicit but does not
// itself introduce concurrency.
//
// Scope is intended to be called from the body of Run or from another
// Scope's body, not from a goroutine spawned via Scope.Go.
//
// The opts parameter is reserved for future use; currently no options are
// defined and any provided values are ignored.
//
// Calling Scope after Run has returned panics, matching the behavior of
// Scope.Go.
func (s *Scope) Scope(body func(child *Scope) error, opts ...Option) {
	s.cond.L.Lock()
	if s.closed {
		s.cond.L.Unlock()
		panic("scope: misuse: Scope called outside scope lifetime")
	}
	s.cond.L.Unlock()

	if err := Run(s.ctx, body); err != nil {
		s.errOnce.Do(func() {
			s.err = err
			s.cancel()
		})
	}
}
