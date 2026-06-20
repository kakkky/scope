package scope

// Option configures a Scope created by Run or Scope.Scope.
type Option func(*options)

// options holds the configuration applied by Option values.
type options struct {
	supervisor     bool
	errAggregation bool
	maxConcurrency int // 0 means no limit
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
