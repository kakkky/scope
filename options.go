package scope

// Option configures a Scope created by Run or Scope.Scope.
type Option func(*options)

// options holds the configuration applied by Option values.
type options struct {
	supervisor bool
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
func WithSupervisor() Option {
	return func(o *options) {
		o.supervisor = true
	}
}
