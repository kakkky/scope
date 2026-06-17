# scope

[![CI](https://github.com/kakkky/scope/actions/workflows/ci.yml/badge.svg)](https://github.com/kakkky/scope/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/kakkky/scope.svg)](https://pkg.go.dev/github.com/kakkky/scope)
[![Go Report Card](https://goreportcard.com/badge/github.com/kakkky/scope)](https://goreportcard.com/report/github.com/kakkky/scope)
[![codecov](https://codecov.io/gh/kakkky/scope/branch/main/graph/badge.svg)](https://codecov.io/gh/kakkky/scope)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Scope-bound structured concurrency for Go.

`scope` binds the lifetime of every goroutine you spawn to a lexical block. When the block exits, every goroutine has either returned or been cancelled — no leaks, no forgotten `Wait()` calls, no panics that crash the process.

## Install

```sh
go get github.com/kakkky/scope
```

Requires Go 1.25 or later.

## Quick Start

```go
package main

import (
    "context"
    "fmt"

    "github.com/kakkky/scope"
)

func main() {
    err := scope.Run(context.Background(), func(s *scope.Scope) error {
        s.Go(func(ctx context.Context) error {
            return ...
        })
        s.Go(func(ctx context.Context) error {
            return ...
        })
        return nil
    })
    // By the time we get here, both goroutines have finished.

    if err != nil {
        fmt.Println("error:", err)
    }
}
```

The `scope.Run` block is a hard boundary: it does not return until every goroutine spawned via `s.Go` has finished. When the first non-nil error occurs, the scope's context is cancelled so sibling goroutines can observe `ctx.Done()` and exit; panics are recovered into errors, and cancellation propagates downstream.

## What it gives you

- **Lifetime bound to a block.** Goroutines spawned with `s.Go` cannot outlive the enclosing `scope.Run` call. There is no `Wait()` to forget — the boundary is the closing brace.
- **First-error-wins, with context cancellation.** Every goroutine receives the scope's context as an argument. When any goroutine returns a non-nil error, the context is cancelled so siblings can observe `ctx.Done()` and exit.
- **Panic safety.** A panic in any spawned goroutine is recovered, formatted with its stack trace, and returned through the same error path. Your process does not crash.
- **Nested scopes.** `s.Scope` creates a child scope whose context is derived from the parent's. The call is synchronous — it blocks until the child's body and all goroutines within it finish. Cancellation propagates from parent to child; errors in the child propagate back and cancel the parent's context.
- **Dynamic spawning.** `s.Go` may be called from inside other goroutines, recursively, or after the body has returned — all of them are still bound to the same scope.


## Why not `errgroup` or raw goroutines?

`golang.org/x/sync/errgroup` covers most of the same ground, but you have to remember to call `Wait()`, you have to pass `ctx` to children yourself, and an unhandled panic still tears the process down. `scope` removes those failure modes by tying the wait to the closure, forcing `ctx` into the signature of every spawned function, and recovering panics by default.

Raw `go func() { ... }()` calls do none of this. They are also the single biggest source of goroutine leaks in real Go codebases.

## Documentation

Full API reference: [pkg.go.dev/github.com/kakkky/scope](https://pkg.go.dev/github.com/kakkky/scope)

## License

MIT — see [LICENSE](./LICENSE).
