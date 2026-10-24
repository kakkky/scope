# scope

[![CI](https://github.com/kakkky/scope/actions/workflows/ci.yml/badge.svg)](https://github.com/kakkky/scope/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/kakkky/scope.svg)](https://pkg.go.dev/github.com/kakkky/scope)
[![Go Report Card](https://goreportcard.com/badge/github.com/kakkky/scope)](https://goreportcard.com/report/github.com/kakkky/scope)
[![codecov](https://codecov.io/gh/kakkky/scope/branch/main/graph/badge.svg)](https://codecov.io/gh/kakkky/scope)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Scope-bound structured concurrency for Go.

`scope` binds the lifetime of every goroutine you spawn to a lexical block. When the block exits, every goroutine has either returned or been cancelled — no leaks, no panics that crash the process.

## Install

```sh
go get github.com/kakkky/scope
```

Requires Go 1.27 or later. If you are on an older Go version, use `v0.8.0`.

## Quick Start

```go
err := scope.Run(context.Background(), func(s *scope.Scope) error {
    s.Go(func(ctx context.Context) error {
        return doA(ctx)
    })
    s.Go(func(ctx context.Context) error {
        return doB(ctx)
    })
    return nil
})
// By the time we get here, both goroutines have finished.
```

`scope.Run` does not return until every goroutine spawned via `s.Go` has finished. When the first non-nil error occurs, the scope's context is cancelled so sibling goroutines can observe `ctx.Done()` and exit. Panics are recovered into errors and returned through the same path.

## API Reference

For full documentation, see [pkg.go.dev/github.com/kakkky/scope](https://pkg.go.dev/github.com/kakkky/scope).

### `Run`

```go
func Run(ctx context.Context, body func(s *Scope) error, opts ...Option) error
```

Establishes a scope, executes `body`, and waits for all goroutines spawned via `s.Go` to finish before returning. The scope's context is derived from `ctx` and cancelled when any goroutine or `body` returns a non-nil error, or when a panic is recovered.

### `Scope.Go`

```go
func (s *Scope) Go(fn func(ctx context.Context) error)
```

Spawns `fn` in a new goroutine bound to the scope. `fn` receives the scope's context so it can participate in cancellation. `Go` may be called from within `body`, from another goroutine spawned by the same scope, or recursively — all goroutines remain bound to the enclosing `Run`.

### `Scope.GoFuture[T]`

```go
func (s *Scope) GoFuture[T any](fn func(ctx context.Context) (T, error)) Future[T]
```

Spawns `fn` in a new goroutine and returns a `Future[T]` to retrieve the result. `future.Wait()` blocks until `fn` has finished and returns the value and error returned by `fn` (or an error describing a panic). It can be called any number of times, from any goroutine, including after `Run` has returned.

### `Scope.Scope`

```go
func (s *Scope) Scope(body func(child *Scope) error, opts ...Option)
```

Creates a child scope nested under `s` and runs `body` synchronously within it. The call blocks until `body` and all goroutines spawned in the child have finished. Cancellation propagates from parent to child; errors in the child propagate back and cancel the parent's context.

### Options

Options are passed to `Run` or `Scope.Scope` to configure the scope. Each scope opts in independently; options are not inherited by child scopes.

#### `WithSupervisor`

```go
func WithSupervisor() Option
```

In supervisor mode, a failure in one goroutine or child scope does not cancel the scope's context, so siblings keep running. Whether a failed child scope cancels its parent is determined by the parent's mode only. Each goroutine runs to completion regardless of others' results.

#### `WithErrAggregation`

```go
func WithErrAggregation() Option
```

By default, only the first error is returned (first-error-wins). With this option, all errors from goroutines and `body` are collected and returned as a single error via `errors.Join`. Most useful in combination with `WithSupervisor`.

#### `WithMaxConcurrency`

```go
func WithMaxConcurrency(max int) Option
```

Limits the number of goroutines that may execute concurrently within the scope. `Go` never blocks: when the limit is reached, the spawned goroutine waits for a running goroutine to complete before running `fn`. If the scope's context is cancelled while waiting, `fn` is never run.

#### `WithTimeout`

```go
func WithTimeout(d time.Duration) Option
```

Cancels the scope's context after duration `d`. When the timeout fires, `Run` returns `context.DeadlineExceeded`. If an earlier deadline is already set on the parent context, that deadline takes precedence.

If the context passed to `Run` has an earlier deadline, this timeout has no effect, and that deadline's error is not returned by `Run` unless something in the scope returns it. When the timeout fires, `context.Cause` of the scope's context is an error wrapping `context.DeadlineExceeded` (`ctx.Err()` is still `context.DeadlineExceeded`).

#### `WithCancelOnSuccess`

```go
func WithCancelOnSuccess() Option
```

Cancels the scope's context as soon as any goroutine or child scope returns a nil error, allowing sibling goroutines to observe `ctx.Done()` and exit early. Errors returned by goroutines or child scopes after this cancellation (typically `ctx.Err()`) are discarded; errors that occurred before it and errors returned by `body` are still returned by `Run`, and panics are never discarded by this option.

### Errors and panics

By default, `Run` returns only the first error (first-error-wins). With `WithErrAggregation`, it returns all errors joined via `errors.Join`. A recovered panic takes precedence over ordinary errors.

| Situation | Effect on the scope | Returned by `Run` |
|---|---|---|
| A goroutine returns an error | Cancels the scope (not with `WithSupervisor`) | Yes |
| A goroutine panics | Recovers it into an error `scope: panic recovered: <value>\n<stack>` and cancels the scope (not with `WithSupervisor`) | Yes, in preference to ordinary errors. For `GoFuture`, `Future.Wait` also returns it |
| The scope's `body` returns an error | Cancels the scope, even with `WithSupervisor` | Yes |
| The scope's `body` panics | Not recovered. Cancels the scope and waits for all its goroutines | No. The panic propagates to the caller of `Run` |
| A goroutine in a child scope returns an error | The child scope records the error, cancels itself (not with `WithSupervisor` on the child scope), and returns the error. The parent scope records it and cancels itself (not with `WithSupervisor` on the parent scope) | Yes |
| A goroutine in a child scope panics | The child scope recovers it into an error, records it, cancels itself (not with `WithSupervisor` on the child scope), and returns the error. The parent scope records it as a panic and cancels itself (not with `WithSupervisor` on the parent scope) | Yes, in preference to ordinary errors |
| A child scope's `body` returns an error | The child scope records the error, cancels itself (even with `WithSupervisor` on the child scope), and returns the error. The parent scope records it and cancels itself (not with `WithSupervisor` on the parent scope) | Yes |
| A child scope's `body` panics | Not recovered. The child scope cancels itself and waits for all its goroutines. The panic propagates to the parent scope's `body`, and the parent scope likewise cancels itself and waits for all its goroutines without recovering it | No. The panic propagates to the caller of `Run` |
| A goroutine or child scope returns an error after a success-triggered cancellation by `WithCancelOnSuccess` | The error is not recorded, since it is typically `ctx.Err()` (`context.Canceled`) returned by a goroutine that stopped because of the cancellation | No, except for panics, which are recorded and returned |
| The scope's own `WithTimeout` fires | Cancels the scope regardless of `WithSupervisor` | `context.DeadlineExceeded`, unless a panic was recovered |
| The deadline of the context passed to `Run` comes first | The scope's context is cancelled as well | The deadline's error is not returned unless something in the scope returns it (a goroutine returning `ctx.Err()` makes `Run` return `context.DeadlineExceeded`) |

### Options and child scopes

Options apply only to the scope they are passed to and are not inherited by child scopes. From the parent scope's point of view, a child scope created via `Scope.Scope` is handled like a single goroutine (except that a panic in the child scope's `body` is not recovered and propagates to the parent scope's `body`). How the parent scope reacts to the child scope's result is determined by the parent scope's options; the child scope's options only determine what the child scope returns.

| Option | Passed to the parent scope | Passed to the child scope |
|---|---|---|
| `WithSupervisor` | A child scope returning an error does not cancel the parent scope | Sibling goroutines in the child scope are not cancelled. The parent scope is still cancelled unless it is a supervisor itself |
| `WithErrAggregation` | The child scope's error is collected together with other errors | The child scope returns all of its errors joined |
| `WithCancelOnSuccess` | A child scope returning without error counts as a success and cancels the parent scope | A success inside the child scope cancels only the child scope |
| `WithTimeout` | Bounds the deadline of all descendants | Can only shorten the deadline within the parent scope's bound. When it fires, the child scope returns `context.DeadlineExceeded` as an error |
| `WithMaxConcurrency` | A child scope does not occupy a slot of the parent scope | Limits only goroutines spawned via `Go` on the child scope |

## Background

This package is inspired by the structured concurrency concept:
- [Structured Concurrency — Martin Sústrik](https://www.250bpm.com/p/structured-concurrency)
- [Notes on structured concurrency, or: Go statement considered harmful — Nathaniel J. Smith](https://vorpus.org/blog/notes-on-structured-concurrency-or-go-statement-considered-harmful)

## License

MIT — see [LICENSE](./LICENSE).
