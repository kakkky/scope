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

Requires Go 1.25 or later.

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

### `GoFuture[T]`

```go
func GoFuture[T any](s *Scope, fn func(ctx context.Context) (T, error)) Future[T]
```

Spawns `fn` in a new goroutine and returns a `Future[T]` to retrieve the result. `future.Wait()` blocks until the value is available or the scope's context is cancelled.

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

In supervisor mode, a failure in one goroutine does not cancel the context of sibling goroutines. Each goroutine runs to completion regardless of others' results.

#### `WithErrAggregation`

```go
func WithErrAggregation() Option
```

By default, only the first error is returned (first-error-wins). With this option, all errors from goroutines and `body` are collected and returned as a single error via `errors.Join`. Most useful in combination with `WithSupervisor`.

#### `WithMaxConcurrency`

```go
func WithMaxConcurrency(max int) Option
```

Limits the number of goroutines that may execute concurrently within the scope. When the limit is reached, calls to `Go` block until a running goroutine completes or the scope's context is cancelled.

#### `WithTimeout`

```go
func WithTimeout(d time.Duration) Option
```

Cancels the scope's context after duration `d`. When the timeout fires, `Run` returns `context.DeadlineExceeded`. If an earlier deadline is already set on the parent context, that deadline takes precedence.

#### `WithCancelOnSuccess`

```go
func WithCancelOnSuccess() Option
```

Cancels the scope's context as soon as any goroutine returns a nil error, allowing sibling goroutines to observe `ctx.Done()` and exit early. `Run` returns nil when this option triggers the cancellation.

## Background

This package is inspired by the structured concurrency concept:
- [Structured Concurrency — Martin Sústrik](https://www.250bpm.com/p/structured-concurrency)
- [Notes on structured concurrency, or: Go statement considered harmful — Nathaniel J. Smith](https://vorpus.org/blog/notes-on-structured-concurrency-or-go-statement-considered-harmful)

## License

MIT — see [LICENSE](./LICENSE).
