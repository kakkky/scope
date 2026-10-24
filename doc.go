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

# Options and child scopes

Options passed to Run or Scope.Scope apply only to the scope they are passed to. They are not
inherited by child scopes, and the options of a child scope never change how its parent
behaves.

From the parent's point of view, a child scope created via Scope.Scope is handled like a
single goroutine spawned via Scope.Go, and the parent reacts to the child's result according
to the parent's own options: WithSupervisor decides whether a failed child cancels the
parent's context, WithErrAggregation decides whether the child's error is collected together
with other errors, and WithCancelOnSuccess treats a child that returns without error as a
success. The options of the child only determine what the child returns, such as a joined
error under WithErrAggregation or context.DeadlineExceeded under WithTimeout.

Cancellation flows from parent to child only: canceling a scope cancels all of its
descendants through the context, while a child affects its parent only through the result it
returns. In particular, WithTimeout on the parent bounds all of its descendants; WithTimeout on
a child can only shorten the deadline within that bound.

The limit set by WithMaxConcurrency counts only goroutines spawned via Go on that scope. A
child scope runs synchronously in the caller's goroutine and does not occupy a slot of its
parent's limit.
*/
package scope
