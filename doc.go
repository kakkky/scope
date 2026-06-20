/*
Package scope provides scope-bound structured concurrency primitives for Go.

Structured concurrency brings the essence of structured programming to concurrent code:
child tasks must not outlive the scope of their parent. This constraint prevents goroutine
leaks, which are among the most common concurrency bugs in Go.

While sync.WaitGroup and golang.org/x/sync/errgroup can achieve similar guarantees via
Wait(), scope takes a different approach by binding goroutine lifetimes to lexical scopes.
Because the scope boundary is visible in the code structure itself, goroutine lifetimes
become easier to reason about at a glance.

The primary entry point is Run. Run establishes a scope and invokes a body function. Inside
the body, Scope.Go spawns goroutines. Run does not return until all spawned goroutines have
completed. Child scopes can be created with Scope.Scope.

Errors and panics from spawned goroutines propagate to the caller of Run. The first non-nil
error cancels the scope's context, which is passed to every goroutine and should be monitored
for cooperative cancellation.
*/
package scope
