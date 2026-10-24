package scope

import "time"

// Option configures a Scope created by Run or Scope.Scope.
type Option func(*options)

// options holds the configuration applied by Option values.
type options struct {
	supervisor      bool
	errAggregation  bool
	maxConcurrency  int // 0 means no limit
	timeout         time.Duration
	cancelOnSuccess bool
}

// WithSupervisor returns an Option that enables supervisor mode for the scope.
//
// In supervisor mode, a failure in one goroutine or child scope does not
// cancel the scope's context, so sibling goroutines and child scopes keep
// running.
//
// Whether a failed child scope created via Scope.Scope cancels its parent is
// determined by the parent's mode only. WithSupervisor passed to the child
// affects only goroutines within the child; a failure in it still cancels a
// parent that is not in supervisor mode.
//
// Note that a non-nil error returned directly from the body function still
// cancels the scope's context regardless of this option.
//
// Goroutines spawned via Go from within another goroutine are registered
// on the same scope and treated as siblings in terms of context cancellation.
//
// This option is not inherited by child scopes created via Scope.Scope;
// each scope must opt in independently.
func WithSupervisor() Option {
	return func(o *options) {
		o.supervisor = true
	}
}

// WithErrAggregation returns an Option that enables error aggregation for the scope.
//
// By default, only the first non-nil error is recorded (first-error-wins).
// With this option, all errors from goroutines and the body are collected and
// returned as a single error via errors.Join when Run returns.
//
// This option is not inherited by child scopes created via Scope.Scope;
// each scope must opt in independently.
//
// WithErrAggregation is most useful in combination with WithSupervisor,
// where goroutines continue running after a sibling fails and multiple
// errors can accumulate.
func WithErrAggregation() Option {
	return func(o *options) {
		o.errAggregation = true
	}
}

// WithMaxConcurrency returns an Option that limits the number of goroutines
// that may execute concurrently within the scope.
//
// Go never blocks: when the limit is reached, the spawned goroutine waits for
// a running goroutine to complete before running fn. If the scope's context is
// canceled while waiting, fn is never run. The limit applies to fn executions,
// not to goroutines waiting for a slot.
//
// This option is not inherited by child scopes created via Scope.Scope;
// each scope must opt in independently.
func WithMaxConcurrency(max int) Option {
	return func(o *options) {
		o.maxConcurrency = max
	}
}

// WithTimeout returns an Option that cancels the scope's context after duration d,
// causing all goroutines in the scope to observe ctx.Done().
//
// When the timeout fires, Run returns context.DeadlineExceeded. The deadline is
// visible via ctx.Deadline() within the scope's goroutines, allowing downstream
// context-aware operations (e.g. HTTP clients) to observe the remaining time.
//
// If an earlier deadline is already set on the context, that deadline takes
// precedence and this timeout has no additional effect.
//
// A timeout always cancels the scope's context regardless of WithSupervisor.
//
// The timeout setting itself is not inherited by child scopes created via Scope.Scope;
// each scope must opt in independently. Cancellation, however, propagates to child
// scopes as with any context cancellation.
func WithTimeout(d time.Duration) Option {
	return func(o *options) {
		o.timeout = d
	}
}

// WithCancelOnSuccess returns an Option that cancels the scope's context as soon
// as any spawned goroutine returns a nil error.
//
// A child scope created via Scope.Scope counts as a success when it returns
// without error, in the same way as a goroutine spawned via Go.
//
// By default, the scope's context is only canceled when a goroutine returns a
// non-nil error. With this option, a successful return also triggers
// cancellation, allowing sibling goroutines to observe ctx.Done() and exit
// early.
//
// Run returns nil when this option triggers the cancellation, even if
// other goroutines subsequently return ctx.Err().
//
// This option is not inherited by child scopes created via Scope.Scope;
// each scope must opt in independently. Cancellation, however, propagates to
// child scopes as with any context cancellation.
func WithCancelOnSuccess() Option {
	return func(o *options) {
		o.cancelOnSuccess = true
	}
}
