package scope

// Option configures a Scope created by Run or Scope.Scope.
//
// No options are currently defined. The Option type exists so that future
// configuration (such as timeouts or error policies) can be added without
// breaking existing callers.
type Option func(*options)

// options holds the configuration applied by Option values.
//
// It is intentionally empty for now and will gain fields as Options are
// introduced.
type options struct {
	// TODO: add fields as Options are introduced.
}
