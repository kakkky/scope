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
// cancel the context of sibling goroutines or the parent scope.
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
// When the limit is reached, calls to Go block until a running goroutine
// completes or the scope's context is canceled.
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
