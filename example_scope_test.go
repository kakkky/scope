package scope_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/kakkky/scope"
)

// ExampleRun shows a minimal usage: spawn two goroutines and wait for both.
func ExampleRun() {
	ctx := context.Background()

	err := scope.Run(ctx, func(s *scope.Scope) error {
		s.Go(func(ctx context.Context) error {
			fmt.Println("task A")
			return nil
		})
		s.Go(func(ctx context.Context) error {
			fmt.Println("task B")
			return nil
		})
		return nil
	})

	if err != nil {
		fmt.Println("err:", err)
	}

	// Unordered output:
	// task A
	// task B
}

// ExampleRun_errorPropagation shows how the first non-nil error cancels
// the scope and is returned to the caller.
func ExampleRun_errorPropagation() {
	ctx := context.Background()

	err := scope.Run(ctx, func(s *scope.Scope) error {
		s.Go(func(ctx context.Context) error {
			return errors.New("task A failed")
		})
		s.Go(func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})
		return nil
	})

	fmt.Println("err:", err)

	// Output: err: task A failed
}

// ExampleRun_panicRecovery shows that a panic inside a spawned goroutine is
// recovered and surfaced as an error rather than crashing the process.
func ExampleRun_panicRecovery() {
	ctx := context.Background()

	err := scope.Run(ctx, func(s *scope.Scope) error {
		s.Go(func(ctx context.Context) error {
			panic("something bad")
		})
		return nil
	})

	// err contains "scope: panic recovered: something bad" followed by a stack trace.
	fmt.Println(err != nil)

	// Output: true
}

// ExampleScope_Go_dynamicSpawn shows that goroutines can be spawned dynamically
// from within other goroutines, as long as Run has not yet returned.
func ExampleScope_Go_dynamicSpawn() {
	ctx := context.Background()
	var count atomic.Int64

	err := scope.Run(ctx, func(s *scope.Scope) error {
		s.Go(func(ctx context.Context) error {
			for i := 0; i < 3; i++ {
				s.Go(func(ctx context.Context) error {
					count.Add(1)
					return nil
				})
			}
			return nil
		})
		return nil
	})

	fmt.Println("err:", err)
	fmt.Println("count:", count.Load())

	// Output:
	// err: <nil>
	// count: 3
}

// ExampleScope_Scope shows how a child scope groups work and blocks until
// every goroutine spawned inside it has finished.
func ExampleScope_Scope() {
	ctx := context.Background()

	err := scope.Run(ctx, func(s *scope.Scope) error {
		s.Scope(func(child *scope.Scope) error {
			child.Go(func(ctx context.Context) error {
				fmt.Println("inside child scope")
				return nil
			})
			return nil
		})
		fmt.Println("after child scope")
		return nil
	})

	fmt.Println("err:", err)

	// Output:
	// inside child scope
	// after child scope
	// err: <nil>
}

