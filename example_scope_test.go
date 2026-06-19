package scope_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// ExampleRun_errorCancelsContext shows how the first non-nil error cancels
// the scope's context so sibling goroutines can observe ctx.Done() and exit.
func ExampleRun_errorCancelsContext() {
	ctx := context.Background()

	err := scope.Run(ctx, func(s *scope.Scope) error {
		s.Go(func(ctx context.Context) error {
			return errors.New("task A failed")
		})
		s.Go(func(ctx context.Context) error {
			<-ctx.Done()
			fmt.Println("task B: context cancelled")
			return ctx.Err()
		})
		return nil
	})

	fmt.Println("err:", err)

	// Output:
	// task B: context cancelled
	// err: task A failed
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

	fmt.Println(strings.Contains(err.Error(), "scope: panic recovered: something bad"))

	// Output: true
}

func ExampleRun_withSupervisor() {
	ctx := context.Background()
	ch := make(chan struct{})

	scope.Run(ctx, func(s *scope.Scope) error {
		s.Go(func(ctx context.Context) error {
			close(ch)
			return errors.New("task failed")
		})
		s.Go(func(ctx context.Context) error {
			<-ch
			fmt.Println("ctx cancelled:", ctx.Err() != nil)
			return nil
		})
		return nil
	}, scope.WithSupervisor())

	// Output:
	// ctx cancelled: false
}

// ExampleScope_Go_dynamicSpawn shows that goroutines can be spawned dynamically
// from within other goroutines, as long as Run has not yet returned.
func ExampleScope_Go_dynamicSpawn() {
	ctx := context.Background()
	var count atomic.Int64

	err := scope.Run(ctx, func(s *scope.Scope) error {
		s.Go(func(ctx context.Context) error {
			for range 3 {
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

// In supervisor mode, a goroutine spawned from within another goroutine is
// treated as a sibling at the scope level in terms of context cancellation.
// Even if the spawning goroutine fails, neither the inner goroutine nor
// sibling goroutines have their context cancelled.
func ExampleScope_Go_errorOccuredFromInnerGoRunWithSupervisor() {
	ctx := context.Background()
	ch1 := make(chan struct{})
	ch2 := make(chan struct{})

	scope.Run(ctx, func(s *scope.Scope) error {
		s.Go(func(ctx context.Context) error {
			s.Go(func(ctx context.Context) error {
				<-ch1
				fmt.Println("inner ctx cancelled:", ctx.Err() != nil)
				close(ch2)
				return nil
			})
			close(ch1)
			return errors.New("task failed")
		})
		s.Go(func(ctx context.Context) error {
			<-ch2
			fmt.Println("sibling ctx cancelled:", ctx.Err() != nil)
			return nil
		})
		return nil
	}, scope.WithSupervisor())

	// Output:
	// inner ctx cancelled: false
	// sibling ctx cancelled: false
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

// ExampleScope_Scope_errorCancelsContext shows that an error in a child scope
// cancels the parent's context so sibling goroutines can observe ctx.Done() and exit.
func ExampleScope_Scope_errorCancelsContext() {
	ctx := context.Background()

	err := scope.Run(ctx, func(s *scope.Scope) error {
		s.Go(func(ctx context.Context) error {
			<-ctx.Done()
			fmt.Println("sibling: context cancelled")
			return ctx.Err()
		})
		s.Scope(func(child *scope.Scope) error {
			return errors.New("child scope failed")
		})
		return nil
	})

	fmt.Println("err:", err)

	// Output:
	// sibling: context cancelled
	// err: child scope failed
}

// ExampleRun_withErrAggregation shows that all goroutine errors are collected
// when WithErrAggregation is enabled. Without it, only the first error is returned.
func ExampleRun_withErrAggregation() {
	errA := errors.New("task A failed")
	errB := errors.New("task B failed")

	err := scope.Run(context.Background(), func(s *scope.Scope) error {
		s.Go(func(ctx context.Context) error { return errA })
		s.Go(func(ctx context.Context) error { return errB })
		return nil
	}, scope.WithErrAggregation())

	fmt.Println(errors.Is(err, errA))
	fmt.Println(errors.Is(err, errB))

	// Output:
	// true
	// true
}

// ExampleScope_Scope_withErrAggregation_parentAndChild shows that each scope
// must opt in to WithErrAggregation independently — it is not inherited.
// When both parent and child have WithErrAggregation, all errors from both
// levels are visible in the result.
func ExampleScope_Scope_errAggregationParentAndChild() {
	errA := errors.New("task A failed")
	errB := errors.New("task B failed")
	errC := errors.New("task C failed")

	err := scope.Run(context.Background(), func(s *scope.Scope) error {
		s.Go(func(ctx context.Context) error { return errA })
		s.Scope(func(child *scope.Scope) error {
			child.Go(func(ctx context.Context) error { return errB })
			child.Go(func(ctx context.Context) error { return errC })
			return nil
		}, scope.WithErrAggregation())
		return nil
	}, scope.WithErrAggregation())

	fmt.Println(errors.Is(err, errA))
	fmt.Println(errors.Is(err, errB))
	fmt.Println(errors.Is(err, errC))

	// Output:
	// true
	// true
	// true
}

// ExampleScope_Scope_errAggregationChildOnly shows that when only the child
// scope has WithErrAggregation, the child aggregates its errors into a single
// joined error, which the root records under first-error-wins policy.
// Since Scope is synchronous, the child's joined error is always recorded first;
// the root's Go error arrives later and is discarded.
func ExampleScope_Scope_errAggregationChildOnly() {
	errA := errors.New("task A failed")
	errB := errors.New("task B failed")
	errC := errors.New("task C failed")

	err := scope.Run(context.Background(), func(s *scope.Scope) error {
		s.Scope(func(child *scope.Scope) error {
			child.Go(func(ctx context.Context) error { return errA })
			child.Go(func(ctx context.Context) error { return errB })
			return nil
		}, scope.WithErrAggregation())
		s.Go(func(ctx context.Context) error { return errC })
		return nil
	})

	fmt.Println(errors.Is(err, errA))
	fmt.Println(errors.Is(err, errB))
	fmt.Println(errors.Is(err, errC))

	// Output:
	// true
	// true
	// false
}

// ExampleScope_Scope_errAggregationParentOnly shows that when only the
// parent has WithErrAggregation, the child uses first-error-wins and propagates
// only one error to the parent. The root also has its own Go, so the root
// aggregates both: the single error from the child and the error from s.Go.
func ExampleScope_Scope_errAggregationParentOnly() {
	errA := errors.New("task A failed")
	errB := errors.New("task B failed")
	errC := errors.New("task C failed")

	err := scope.Run(context.Background(), func(s *scope.Scope) error {
		s.Scope(func(child *scope.Scope) error {
			child.Go(func(ctx context.Context) error { return errA })
			child.Go(func(ctx context.Context) error { return errB })
			return nil
		})
		s.Go(func(ctx context.Context) error { return errC })
		return nil
	}, scope.WithErrAggregation())

	// root aggregates: one of errA/errB (child's first-error-wins) and errC
	fmt.Println((errors.Is(err, errA) || errors.Is(err, errB)) && errors.Is(err, errC))

	// Output:
	// true
}

// ExampleRun_withSupervisorAndErrAggregation shows that combining WithSupervisor
// and WithErrAggregation allows collecting all errors from goroutines that run
// to completion without being cancelled by sibling failures.
func ExampleRun_withSupervisorAndErrAggregation() {
	errA := errors.New("task A failed")
	errB := errors.New("task B failed")

	err := scope.Run(context.Background(), func(s *scope.Scope) error {
		s.Go(func(ctx context.Context) error { return errA })
		s.Go(func(ctx context.Context) error { return errB })
		return nil
	}, scope.WithSupervisor(), scope.WithErrAggregation())

	fmt.Println(errors.Is(err, errA))
	fmt.Println(errors.Is(err, errB))

	// Output:
	// true
	// true
}

// In supervisor mode, goroutine failures within a child scope do not cancel
// sibling goroutines inside that scope, nor goroutines in the parent scope.
func ExampleScope_Scope_errorOccuredFromGoWithSupervisor() {
	ctx := context.Background()
	ch1 := make(chan struct{})

	err := scope.Run(ctx, func(s *scope.Scope) error {
		s.Scope(func(child *scope.Scope) error {
			child.Go(func(ctx context.Context) error {
				close(ch1)
				return errors.New("task failed")
			})
			child.Go(func(ctx context.Context) error {
				<-ch1
				fmt.Println("sibling ctx cancelled:", ctx.Err() != nil)
				return nil
			})
			return nil
		}, scope.WithSupervisor())
		s.Go(func(ctx context.Context) error {
			fmt.Println("parent level goroutine ctx cancelled:", ctx.Err() != nil)
			return nil
		})
		return nil
	})

	fmt.Println("err:", err)

	// Output:
	// sibling ctx cancelled: false
	// parent level goroutine ctx cancelled: false
	// err: task failed
}
