package scope

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"

	"golang.org/x/sync/semaphore"
)

var errCanceledOnSuccess = errors.New("scope: canceled on first success")

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
	cancel context.CancelCauseFunc

	cond    *sync.Cond
	activeG int
	closed  bool

	errOnce sync.Once
	err     error

	supervisor bool

	errAggregation bool
	errsMu         sync.Mutex
	errs           []error

	sem *semaphore.Weighted

	cancelOnSuccess bool
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
func Run(ctx context.Context, body func(s *Scope) error, opts ...Option) error {
	return run(ctx, body, opts...)
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
	s.spawnGoroutine(fn)
}

// GoFuture starts fn in a new goroutine bound to the scope and returns a Future
// that can be used to retrieve the result via Future.Wait.
//
// If fn returns a non-nil error, the error is propagated through the scope's
// error handling mechanism (same as Go) and Wait will unblock via ctx cancellation.
//
// Note: this is a package-level function rather than a method on Scope because
// Go does not currently support generic methods. Once generic methods are
// supported, this may be promoted to s.GoFuture(...).
func GoFuture[T any](s *Scope, fn func(ctx context.Context) (T, error)) Future[T] {
	future := newFuture[T](s.ctx)
	s.spawnGoroutine(func(ctx context.Context) error {
		v, err := fn(ctx)
		if err != nil {
			return err
		}
		future.set(v)
		return nil
	})
	return future
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
// Calling Scope after Run has returned panics, matching the behavior of
// Scope.Go.
func (s *Scope) Scope(body func(child *Scope) error, opts ...Option) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	s.cond.L.Lock()
	if s.closed {
		s.cond.L.Unlock()
		panic("scope: misuse: Scope called outside scope lifetime")
	}
	s.cond.L.Unlock()

	if err := run(s.ctx, body, opts...); err != nil {
		s.recordErr(err)
		if !o.supervisor {
			s.cancel(err)
		}
	}
}

// run creates a new Scope, executes body, and waits for all spawned goroutines to finish.
func run(ctx context.Context, body func(s *Scope) error, opts ...Option) error {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	if o.timeout > 0 {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, o.timeout)
		defer timeoutCancel()
	}

	ctx, cancel := context.WithCancelCause(ctx)

	s := &Scope{
		ctx:             ctx,
		cancel:          cancel,
		cond:            sync.NewCond(&sync.Mutex{}),
		supervisor:      o.supervisor,
		errAggregation:  o.errAggregation,
		cancelOnSuccess: o.cancelOnSuccess,
	}

	if o.maxConcurrency > 0 {
		s.sem = semaphore.NewWeighted(int64(o.maxConcurrency))
	}

	defer s.cancel(nil)

	err := body(s)

	if err != nil {
		s.recordErr(err)
		s.cancel(err)
	}

	s.cond.L.Lock()
	for s.activeG > 0 {
		s.cond.Wait()
	}
	s.closed = true
	s.cond.L.Unlock()

	cause := context.Cause(s.ctx)
	switch {
	case o.timeout > 0 && errors.Is(cause, context.DeadlineExceeded):
		if s.errAggregation {
			return errors.Join(append(s.errs, cause)...)
		}
		return cause
	case o.cancelOnSuccess && errors.Is(cause, errCanceledOnSuccess):
		return nil
	case s.errAggregation:
		return errors.Join(s.errs...)
	default:
		return s.err
	}
}

func (s *Scope) spawnGoroutine(fn func(ctx context.Context) error) {
	if s.sem != nil {
		if err := s.sem.Acquire(s.ctx, 1); err != nil {
			// ctx is already canceled; the cause was recorded by the goroutine that triggered cancellation.
			return
		}
	}

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
			if s.sem != nil {
				s.sem.Release(1)
			}
		}()
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("scope: panic recovered: %v\n%s", r, debug.Stack())
				s.recordErr(err)
				if !s.supervisor {
					s.cancel(err)
				}
				return
			}
		}()

		if err := fn(s.ctx); err != nil {
			s.recordErr(err)
			if !s.supervisor {
				s.cancel(err)
			}
		} else if s.cancelOnSuccess {
			s.cancel(errCanceledOnSuccess)
		}
	}()
}

// recordErr stores err according to the aggregation policy.
// In aggregation mode, all errors are collected for errors.Join at the end of Run.
// Otherwise, only the first error is kept via errOnce.
func (s *Scope) recordErr(err error) {
	if s.errAggregation {
		s.errsMu.Lock()
		s.errs = append(s.errs, err)
		s.errsMu.Unlock()
	} else {
		s.errOnce.Do(func() {
			s.err = err
		})
	}
}
