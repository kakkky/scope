package scope_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kakkky/scope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		body      func(s *scope.Scope, count *atomic.Int64) error
		opts      []scope.Option
		wantCount int64
	}{
		{
			name: "empty body returns nil",
			body: func(s *scope.Scope, count *atomic.Int64) error {
				return nil
			},
			wantCount: 0,
		},
		{
			name: "body returns nil but spawns 10",
			body: func(s *scope.Scope, count *atomic.Int64) error {
				for range 10 {
					s.Go(func(ctx context.Context) error {
						count.Add(1)
						return nil
					})
				}
				return nil
			},
			wantCount: 10,
		},
		{
			name: "body increments without spawning",
			body: func(s *scope.Scope, count *atomic.Int64) error {
				count.Add(5)
				return nil
			},
			wantCount: 5,
		},
		{
			name: "empty child body returns nil",
			body: func(s *scope.Scope, count *atomic.Int64) error {
				s.Scope(func(child *scope.Scope) error {
					return nil
				})
				return nil
			},
			wantCount: 0,
		},
		{
			name: "child spawns 10 goroutines, all finish before Scope returns",
			body: func(s *scope.Scope, count *atomic.Int64) error {
				s.Scope(func(child *scope.Scope) error {
					for range 10 {
						child.Go(func(ctx context.Context) error {
							count.Add(1)
							return nil
						})
					}
					return nil
				})
				return nil
			},
			wantCount: 10,
		},
		{
			name: "grandchild scope spawns goroutines at each level",
			body: func(s *scope.Scope, count *atomic.Int64) error {
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error {
						count.Add(1)
						return nil
					})
					child.Scope(func(grandchild *scope.Scope) error {
						for range 3 {
							grandchild.Go(func(ctx context.Context) error {
								count.Add(1)
								return nil
							})
						}
						return nil
					})
					return nil
				})
				return nil
			},
			wantCount: 4,
		},
		{
			name: "parent mixes s.Go and s.Scope as siblings",
			body: func(s *scope.Scope, count *atomic.Int64) error {
				s.Go(func(ctx context.Context) error {
					count.Add(1)
					return nil
				})
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error {
						count.Add(1)
						return nil
					})
					return nil
				})
				return nil
			},
			wantCount: 2,
		},
		{
			name: "sibling Scopes run sequentially",
			body: func(s *scope.Scope, count *atomic.Int64) error {
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error {
						count.Add(1)
						return nil
					})
					return nil
				})
				s.Scope(func(child *scope.Scope) error {
					if count.Load() != 1 {
						return assert.AnError
					}
					child.Go(func(ctx context.Context) error {
						count.Add(1)
						return nil
					})
					return nil
				})
				return nil
			},
			wantCount: 2,
		},
		{
			name: "WithCancelOnSuccess: first success cancels remaining goroutines",
			opts: []scope.Option{scope.WithCancelOnSuccess()},
			body: func(s *scope.Scope, count *atomic.Int64) error {
				s.Go(func(ctx context.Context) error {
					count.Add(1)
					return nil
				})
				s.Go(func(ctx context.Context) error {
					<-ctx.Done()
					return ctx.Err()
				})
				return nil
			},
			wantCount: 1,
		},
		{
			name: "WithCancelOnSuccess: Run returns nil even if remaining goroutines return ctx.Err()",
			opts: []scope.Option{scope.WithCancelOnSuccess()},
			body: func(s *scope.Scope, count *atomic.Int64) error {
				s.Go(func(ctx context.Context) error {
					count.Add(1)
					return nil
				})
				s.Go(func(ctx context.Context) error {
					<-ctx.Done()
					return ctx.Err()
				})
				return nil
			},
			wantCount: 1,
		},
		{
			name: "WithCancelOnSuccess: parent cancel propagates to child scope",
			opts: []scope.Option{scope.WithCancelOnSuccess()},
			body: func(s *scope.Scope, count *atomic.Int64) error {
				s.Go(func(ctx context.Context) error {
					count.Add(1)
					return nil
				})
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error {
						<-ctx.Done()
						return ctx.Err()
					})
					return nil
				})
				return nil
			},
			wantCount: 1,
		},
		{
			name: "WithCancelOnSuccess: child cancel does not propagate to parent",
			body: func(s *scope.Scope, count *atomic.Int64) error {
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error {
						count.Add(1)
						return nil
					})
					child.Go(func(ctx context.Context) error {
						<-ctx.Done()
						return ctx.Err()
					})
					return nil
				}, scope.WithCancelOnSuccess())
				s.Go(func(ctx context.Context) error {
					count.Add(10)
					return ctx.Err() // returns nil if parent ctx is not canceled
				})
				return nil
			},
			wantCount: 11,
		},
		// GoFuture
		{
			name: "GoFuture: returns value produced by goroutine",
			body: func(s *scope.Scope, count *atomic.Int64) error {
				var want = "test"
				f := scope.GoFuture(s, func(ctx context.Context) (string, error) {
					count.Add(1)
					return want, nil
				})
				got, err := f.Wait()
				assert.Equal(t, want, got)
				return err
			},
			wantCount: 1,
		},
		{
			name: "GoFuture: two futures run concurrently and both return correct values",
			body: func(s *scope.Scope, count *atomic.Int64) error {
				var want1 = "test"
				var want2 = "test"
				f1 := scope.GoFuture(s, func(ctx context.Context) (string, error) {
					count.Add(1)
					return want1, nil
				})
				f2 := scope.GoFuture(s, func(ctx context.Context) (string, error) {
					count.Add(1)
					return want2, nil
				})
				got1, err := f1.Wait()
				assert.Equal(t, want1, got1)
				if err != nil {
					return err
				}

				got2, err := f2.Wait()
				assert.Equal(t, want2, got2)
				if err != nil {
					return err
				}
				return nil
			},
			wantCount: 2,
		},
		{
			name: "GoFuture: dag: second future uses result of first future",
			body: func(s *scope.Scope, count *atomic.Int64) error {
				f1 := scope.GoFuture(s, func(ctx context.Context) (int, error) {
					count.Add(1)
					return 1, nil
				})
				f2 := scope.GoFuture(s, func(ctx context.Context) (int, error) {
					got1, err := f1.Wait()
					if err != nil {
						return 0, err
					}
					count.Add(1)
					return 1 + got1, nil
				})
				got2, err := f2.Wait()
				assert.Equal(t, got2, 2)
				if err != nil {
					return err
				}
				return nil
			},
			wantCount: 2,
		},
		{
			name: "GoFuture: future not waited does not cause leak or deadlock",
			body: func(s *scope.Scope, count *atomic.Int64) error {
				_ = scope.GoFuture(s, func(ctx context.Context) (string, error) {
					count.Add(1)
					return "test", nil
				})
				return nil
			},
			wantCount: 1,
		},
		{
			name: "GoFuture: Wait called inside s.Go goroutine",
			body: func(s *scope.Scope, count *atomic.Int64) error {
				f := scope.GoFuture(s, func(ctx context.Context) (int, error) {
					count.Add(1)
					return 42, nil
				})
				s.Go(func(ctx context.Context) error {
					count.Add(1)
					got, err := f.Wait()
					assert.Equal(t, 42, got)
					return err
				})
				return nil
			},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var count atomic.Int64
			err := scope.Run(context.Background(), func(s *scope.Scope) error {
				return tt.body(s, &count)
			}, tt.opts...)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantCount, count.Load())
		})
	}
}

func TestRun_Error(t *testing.T) {
	t.Parallel()

	var (
		errA = errors.New("error A")
		errB = errors.New("error B")
		errC = errors.New("error C")
	)

	tests := []struct {
		name     string
		body     func(s *scope.Scope) error
		opts     []scope.Option
		wantErrs []error
	}{
		{
			name: "body returns error",
			body: func(s *scope.Scope) error {
				return assert.AnError
			},
			wantErrs: []error{assert.AnError},
		},
		{
			name: "body spawns goroutine that returns error",
			body: func(s *scope.Scope) error {
				s.Go(func(ctx context.Context) error {
					return assert.AnError
				})
				return nil
			},
			wantErrs: []error{assert.AnError},
		},
		{
			name: "body spawns goroutines that return error, but only first error is returned",
			body: func(s *scope.Scope) error {
				s.Go(func(ctx context.Context) error {
					return assert.AnError
				})
				s.Go(func(ctx context.Context) error {
					<-ctx.Done()
					return ctx.Err()
				})
				return nil
			},
			wantErrs: []error{assert.AnError},
		},
		{
			name: "grandchild error propagates",
			body: func(s *scope.Scope) error {
				s.Go(func(ctx context.Context) error {
					s.Go(func(ctx context.Context) error {
						return assert.AnError
					})
					return nil
				})
				return nil
			},
			wantErrs: []error{assert.AnError},
		},
		{
			name: "successful children do not interfere with error propagation",
			body: func(s *scope.Scope) error {
				for range 3 {
					s.Go(func(ctx context.Context) error { return nil })
				}
				s.Go(func(ctx context.Context) error {
					return assert.AnError
				})
				return nil
			},
			wantErrs: []error{assert.AnError},
		},
		{
			name: "error in child scope body propagates",
			body: func(s *scope.Scope) error {
				s.Scope(func(child *scope.Scope) error {
					return assert.AnError
				})
				return nil
			},
			wantErrs: []error{assert.AnError},
		},
		{
			name: "error in child scope goroutine propagates",
			body: func(s *scope.Scope) error {
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error {
						return assert.AnError
					})
					return nil
				})
				return nil
			},
			wantErrs: []error{assert.AnError},
		},
		{
			name: "GoFuture fn returns error: error propagates and Wait unblocks",
			body: func(s *scope.Scope) error {
				f := scope.GoFuture(s, func(ctx context.Context) (string, error) {
					return "", assert.AnError
				})
				_, err := f.Wait()
				assert.ErrorIs(t, err, context.Canceled)
				return nil
			},
			wantErrs: []error{assert.AnError},
		},
		// WithErrAggregation
		{
			name: "WithErrAggregation: all goroutine errors are collected",
			body: func(s *scope.Scope) error {
				s.Go(func(ctx context.Context) error { return errA })
				s.Go(func(ctx context.Context) error { return errB })
				return nil
			},
			opts:     []scope.Option{scope.WithErrAggregation()},
			wantErrs: []error{errA, errB},
		},
		{
			name: "WithErrAggregation: body error is collected",
			body: func(s *scope.Scope) error {
				s.Go(func(ctx context.Context) error { return errA })
				return errB
			},
			opts:     []scope.Option{scope.WithErrAggregation()},
			wantErrs: []error{errA, errB},
		},
		{
			name: "WithErrAggregation: goroutine errors in child scope are collected when child also has aggregation",
			body: func(s *scope.Scope) error {
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error { return errA })
					child.Go(func(ctx context.Context) error { return errB })
					return nil
				}, scope.WithErrAggregation())
				return nil
			},
			opts:     []scope.Option{scope.WithErrAggregation()},
			wantErrs: []error{errA, errB},
		},
		{
			name: "WithErrAggregation: child scope collects errors independently of root",
			body: func(s *scope.Scope) error {
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error { return errA })
					child.Go(func(ctx context.Context) error { return errB })
					return nil
				}, scope.WithErrAggregation())
				return nil
			},
			wantErrs: []error{errA, errB},
		},
		{
			// root uses first-error-wins policy; s.Go fires first, so child's joined error is discarded
			name: "WithErrAggregation: root records first error only when s.Go fires before child scope",
			body: func(s *scope.Scope) error {
				ch := make(chan struct{})
				s.Go(func(ctx context.Context) error {
					close(ch)
					return errA
				})
				s.Scope(func(child *scope.Scope) error {
					<-ch
					child.Go(func(ctx context.Context) error { return errA })
					child.Go(func(ctx context.Context) error { return errB })
					return nil
				}, scope.WithErrAggregation())
				return nil
			},
			wantErrs: []error{errA},
		},
		{
			// root uses first-error-wins policy; child scope fires first, so joined error (errA+errB) is recorded
			name: "WithErrAggregation: root records joined child error when child scope fires before s.Go",
			body: func(s *scope.Scope) error {
				ch := make(chan struct{})
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error {
						close(ch)
						return errA
					})
					child.Go(func(ctx context.Context) error { return errB })
					return nil
				}, scope.WithErrAggregation())
				s.Go(func(ctx context.Context) error {
					<-ch
					return errA
				})
				return nil
			},
			wantErrs: []error{errA, errB},
		},
		// WithSupervisor + WithErrAggregation
		{
			name: "WithSupervisor+WithErrAggregation: all goroutine errors are collected",
			body: func(s *scope.Scope) error {
				ch := make(chan struct{})
				s.Go(func(ctx context.Context) error {
					close(ch)
					return errA
				})
				s.Go(func(ctx context.Context) error {
					<-ch
					return errB
				})
				return nil
			},
			opts:     []scope.Option{scope.WithSupervisor(), scope.WithErrAggregation()},
			wantErrs: []error{errA, errB},
		},
		{
			name: "WithSupervisor+WithErrAggregation: nested scopes collect all errors",
			body: func(s *scope.Scope) error {
				s.Go(func(ctx context.Context) error { return errA })
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error { return errB })
					child.Go(func(ctx context.Context) error { return errC })
					return nil
				}, scope.WithSupervisor(), scope.WithErrAggregation())
				return nil
			},
			opts:     []scope.Option{scope.WithSupervisor(), scope.WithErrAggregation()},
			wantErrs: []error{errA, errB, errC},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := scope.Run(t.Context(), tt.body, tt.opts...)
			for _, wantErr := range tt.wantErrs {
				assert.ErrorIs(t, err, wantErr)
			}
		})
	}

	t.Run("WithErrAggregation is not inherited by child scope: only first error propagates", func(t *testing.T) {
		t.Parallel()

		err := scope.Run(t.Context(), func(s *scope.Scope) error {
			s.Scope(func(child *scope.Scope) error {
				child.Go(func(ctx context.Context) error { return errA })
				child.Go(func(ctx context.Context) error { return errB })
				return nil
			})
			return nil
		}, scope.WithErrAggregation())

		isA := errors.Is(err, errA)
		isB := errors.Is(err, errB)
		assert.True(t, isA != isB, "only one of errA or errB should propagate, not both")
	})
}

func TestRun_Cancel(t *testing.T) {
	t.Parallel()

	t.Run("parent cancel propagates to children", func(t *testing.T) {
		t.Parallel()

		parentCtx, parentCancel := context.WithCancel(t.Context())
		defer parentCancel()

		var observed atomic.Bool

		err := scope.Run(parentCtx, func(s *scope.Scope) error {
			s.Go(func(ctx context.Context) error {
				<-ctx.Done()
				observed.Store(true)
				return ctx.Err()
			})
			parentCancel()
			return nil
		})

		assert.True(t, observed.Load(), "child did not observe cancel")
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("body error cancels running children", func(t *testing.T) {
		t.Parallel()

		var observed atomic.Bool

		err := scope.Run(t.Context(), func(s *scope.Scope) error {
			s.Go(func(ctx context.Context) error {
				<-ctx.Done()
				observed.Store(true)
				return ctx.Err()
			})
			return assert.AnError
		})

		assert.True(t, observed.Load(), "child did not observe cancel after body error")
		assert.ErrorIs(t, err, assert.AnError)
	})

	t.Run("derived ctx is cancelled after Run returns", func(t *testing.T) {
		t.Parallel()

		var captured context.Context

		err := scope.Run(context.Background(), func(s *scope.Scope) error {
			s.Go(func(ctx context.Context) error {
				captured = ctx
				return nil
			})
			return nil
		})

		assert.NoError(t, err)
		require.NotNil(t, captured)
		assert.ErrorIs(t, captured.Err(), context.Canceled, "derived ctx should be cancelled after Run returns")
	})
}

func TestRun_PanicInGo(t *testing.T) {
	t.Parallel()

	var observed atomic.Bool

	err := scope.Run(context.Background(), func(s *scope.Scope) error {
		s.Go(func(ctx context.Context) error {
			panic("boom")
		})
		s.Go(func(ctx context.Context) error {
			<-ctx.Done()
			observed.Store(true)
			return ctx.Err()
		})
		return nil
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "boom", "panic value should be in error message")
	assert.Contains(t, err.Error(), "goroutine", "stack trace should be in error message")
	assert.True(t, observed.Load(), "sibling did not observe cancel after panic")

}

func TestRun_CancellationCause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      func(s *scope.Scope, cause *error) error
		opts      []scope.Option
		wantCause string
	}{
		{
			name: "goroutine error sets cause",
			body: func(s *scope.Scope, cause *error) error {
				s.Go(func(ctx context.Context) error {
					return assert.AnError
				})
				s.Go(func(ctx context.Context) error {
					<-ctx.Done()
					*cause = context.Cause(ctx)
					return ctx.Err()
				})
				return nil
			},
			wantCause: assert.AnError.Error(),
		},
		{
			name: "body error sets cause",
			body: func(s *scope.Scope, cause *error) error {
				s.Go(func(ctx context.Context) error {
					<-ctx.Done()
					*cause = context.Cause(ctx)
					return ctx.Err()
				})
				return assert.AnError
			},
			wantCause: assert.AnError.Error(),
		},
		{
			name: "normal completion leaves cause nil",
			body: func(s *scope.Scope, cause *error) error {
				s.Go(func(ctx context.Context) error {
					*cause = context.Cause(ctx)
					return nil
				})
				return nil
			},
			wantCause: "",
		},
		{
			name: "supervisor: context not canceled so cause is nil",
			body: func(s *scope.Scope, cause *error) error {
				s.Go(func(ctx context.Context) error {
					return assert.AnError
				})
				s.Go(func(ctx context.Context) error {
					*cause = context.Cause(ctx)
					return nil
				})
				return nil
			},
			opts:      []scope.Option{scope.WithSupervisor()},
			wantCause: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var cause error
			scope.Run(t.Context(), func(s *scope.Scope) error {
				return tt.body(s, &cause)
			}, tt.opts...)

			got := ""
			if cause != nil {
				got = cause.Error()
			}
			assert.Equal(t, tt.wantCause, got)
		})
	}

	t.Run("panic sets cause", func(t *testing.T) {
		t.Parallel()

		var cause error
		scope.Run(t.Context(), func(s *scope.Scope) error {
			s.Go(func(ctx context.Context) error {
				panic("boom")
			})
			s.Go(func(ctx context.Context) error {
				<-ctx.Done()
				cause = context.Cause(ctx)
				return ctx.Err()
			})
			return nil
		})

		require.NotNil(t, cause)
		assert.Contains(t, cause.Error(), "boom")
	})
}

func TestRun_CallMethodOutsideScopeLifetime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		call      func(s *scope.Scope)
		wantPanic string
	}{
		{
			name: "Go panics when called after Run returned",
			call: func(s *scope.Scope) {
				s.Go(func(ctx context.Context) error { return nil })
			},
			wantPanic: "scope: misuse: Go called outside scope lifetime",
		},
		{
			name: "Scope panics when called after Run returned",
			call: func(s *scope.Scope) {
				s.Scope(func(child *scope.Scope) error { return nil })
			},
			wantPanic: "scope: misuse: Scope called outside scope lifetime",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var captured *scope.Scope
			err := scope.Run(t.Context(), func(s *scope.Scope) error {
				captured = s
				return nil
			})
			assert.NoError(t, err)
			assert.PanicsWithValue(t, tt.wantPanic, func() {
				tt.call(captured)
			})
		})
	}
}

func TestRun_WithSupervisor(t *testing.T) {
	t.Parallel()

	t.Run("goroutine error does not cancel sibling goroutines", func(t *testing.T) {
		t.Parallel()

		ch := make(chan struct{})
		var observed atomic.Bool

		err := scope.Run(t.Context(), func(s *scope.Scope) error {
			s.Go(func(ctx context.Context) error {
				close(ch)
				return assert.AnError
			})
			s.Go(func(ctx context.Context) error {
				<-ch
				observed.Store(ctx.Err() == nil)
				return nil
			})
			return nil
		}, scope.WithSupervisor())

		assert.ErrorIs(t, err, assert.AnError)
		assert.True(t, observed.Load(), "sibling goroutine should not have been cancelled")
	})

	t.Run("panic does not cancel sibling goroutines", func(t *testing.T) {
		t.Parallel()

		ch := make(chan struct{})
		var observed atomic.Bool

		err := scope.Run(t.Context(), func(s *scope.Scope) error {
			s.Go(func(ctx context.Context) error {
				close(ch)
				panic("boom")
			})
			s.Go(func(ctx context.Context) error {
				<-ch
				observed.Store(ctx.Err() == nil)
				return nil
			})
			return nil
		}, scope.WithSupervisor())

		assert.Error(t, err)
		assert.True(t, observed.Load(), "sibling goroutine should not have been cancelled after panic")
	})

	t.Run("body error cancels goroutines even in supervisor mode", func(t *testing.T) {
		t.Parallel()

		var observed atomic.Bool

		err := scope.Run(t.Context(), func(s *scope.Scope) error {
			s.Go(func(ctx context.Context) error {
				<-ctx.Done()
				observed.Store(true)
				return ctx.Err()
			})
			return assert.AnError
		}, scope.WithSupervisor())

		assert.ErrorIs(t, err, assert.AnError)
		assert.True(t, observed.Load(), "goroutine should have been cancelled by body error")
	})

	t.Run("child scope goroutine error does not cancel sibling or parent goroutines", func(t *testing.T) {
		t.Parallel()

		ch1 := make(chan struct{})
		ch2 := make(chan struct{})
		var siblingObserved, parentObserved atomic.Bool

		err := scope.Run(t.Context(), func(s *scope.Scope) error {
			s.Scope(func(child *scope.Scope) error {
				child.Go(func(ctx context.Context) error {
					close(ch1)
					return assert.AnError
				})
				child.Go(func(ctx context.Context) error {
					<-ch1
					siblingObserved.Store(ctx.Err() == nil)
					close(ch2)
					return nil
				})
				return nil
			}, scope.WithSupervisor())
			s.Go(func(ctx context.Context) error {
				<-ch2
				parentObserved.Store(ctx.Err() == nil)
				return nil
			})
			return nil
		})

		assert.ErrorIs(t, err, assert.AnError)
		assert.True(t, siblingObserved.Load(), "sibling goroutine within child scope should not have been cancelled")
		assert.True(t, parentObserved.Load(), "parent goroutine should not have been cancelled")
	})

}

func TestRun_WithMaxConcurrency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		opts        []scope.Option
		body        func(s *scope.Scope, trackFn func(ctx context.Context) error) error
		wantPeakMax int
	}{
		{
			name: "Run only: peak does not exceed max",
			opts: []scope.Option{scope.WithMaxConcurrency(3)},
			body: func(s *scope.Scope, trackFn func(ctx context.Context) error) error {
				for range 10 {
					s.Go(trackFn)
				}
				return nil
			},
			wantPeakMax: 3,
		},
		{
			name: "child scope only has max: peak in child does not exceed max",
			body: func(s *scope.Scope, trackFn func(ctx context.Context) error) error {
				s.Scope(func(child *scope.Scope) error {
					for range 10 {
						child.Go(trackFn)
					}
					return nil
				}, scope.WithMaxConcurrency(2))
				return nil
			},
			wantPeakMax: 2,
		},
		{
			name: "root scope only has max: root max does not apply to child",
			opts: []scope.Option{scope.WithMaxConcurrency(3)},
			body: func(s *scope.Scope, trackFn func(ctx context.Context) error) error {
				s.Scope(func(child *scope.Scope) error {
					for range 10 {
						child.Go(trackFn)
					}
					return nil
				})
				return nil
			},
			wantPeakMax: 10, // child has no max, root max does not apply to child
		},
		{
			name: "both root and child have max: each peak does not exceed its own max",
			opts: []scope.Option{scope.WithMaxConcurrency(3)},
			body: func(s *scope.Scope, trackFn func(ctx context.Context) error) error {
				s.Scope(func(child *scope.Scope) error {
					for range 10 {
						child.Go(trackFn)
					}
					return nil
				}, scope.WithMaxConcurrency(2))
				return nil
			},
			wantPeakMax: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				var mu sync.Mutex
				var current, peak int

				trackFn := func(ctx context.Context) error {
					mu.Lock()
					current++
					if current > peak {
						peak = current
					}
					mu.Unlock()

					time.Sleep(3 * time.Second)

					mu.Lock()
					current--
					mu.Unlock()
					return nil
				}

				err := scope.Run(context.Background(), func(s *scope.Scope) error {
					return tt.body(s, trackFn)
				}, tt.opts...)

				assert.NoError(t, err)
				assert.LessOrEqual(t, peak, tt.wantPeakMax)
			})
		})
	}

	t.Run("semaphore is released on panic so next goroutine can proceed", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			var reached atomic.Bool

			err := scope.Run(context.Background(), func(s *scope.Scope) error {
				s.Go(func(ctx context.Context) error {
					panic("boom")
				})
				s.Go(func(ctx context.Context) error {
					reached.Store(true)
					return nil
				})
				return nil
			}, scope.WithMaxConcurrency(1), scope.WithSupervisor())

			assert.Error(t, err)
			assert.True(t, reached.Load(), "second goroutine should have acquired semaphore after panic released it")
		})
	})

	t.Run("goroutine waiting on semaphore exits cleanly on ctx cancellation", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			var goroutine2Ran atomic.Bool

			err := scope.Run(context.Background(), func(s *scope.Scope) error {
				// first goroutine holds the slot and returns an error to cancel ctx
				s.Go(func(ctx context.Context) error {
					time.Sleep(1 * time.Second)
					return assert.AnError
				})
				// second goroutine blocks on semaphore; Acquire should fail on ctx cancellation
				s.Go(func(ctx context.Context) error {
					goroutine2Ran.Store(true)
					return nil
				})
				return nil
			}, scope.WithMaxConcurrency(1))

			assert.ErrorIs(t, err, assert.AnError)
			assert.False(t, goroutine2Ran.Load(), "goroutine2 should not have run: Acquire should have failed on ctx cancellation")
		})
	})
}

func TestRun_WithTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    func(s *scope.Scope) error
		opts    []scope.Option
		wantErr error
	}{
		{
			name: "root timeout fire: Run returns context.DeadlineExceeded",
			opts: []scope.Option{scope.WithTimeout(1 * time.Second)},
			body: func(s *scope.Scope) error {
				s.Go(func(ctx context.Context) error {
					time.Sleep(3 * time.Second)
					return nil
				})
				return nil
			},
			wantErr: context.DeadlineExceeded,
		},
		{
			name: "root timeout does not fire: Run returns nil",
			opts: []scope.Option{scope.WithTimeout(5 * time.Second)},
			body: func(s *scope.Scope) error {
				s.Go(func(ctx context.Context) error {
					time.Sleep(1 * time.Second)
					return nil
				})
				return nil
			},
			wantErr: nil,
		},
		{
			name: "child only timeout fires: error propagates to parent",
			body: func(s *scope.Scope) error {
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error {
						time.Sleep(3 * time.Second)
						return nil
					})
					return nil
				}, scope.WithTimeout(1*time.Second))
				return nil
			},
			wantErr: context.DeadlineExceeded,
		},
		{
			name: "timeout cancels all goroutines in scope",
			opts: []scope.Option{scope.WithTimeout(1 * time.Second)},
			body: func(s *scope.Scope) error {
				for range 3 {
					s.Go(func(ctx context.Context) error {
						<-ctx.Done()
						return ctx.Err()
					})
				}
				return nil
			},
			wantErr: context.DeadlineExceeded,
		},
		{
			name: "parent timeout < child timeout: parent fires first",
			opts: []scope.Option{scope.WithTimeout(1 * time.Second)},
			body: func(s *scope.Scope) error {
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error {
						time.Sleep(3 * time.Second)
						return nil
					})
					return nil
				}, scope.WithTimeout(5*time.Second))
				return nil
			},
			wantErr: context.DeadlineExceeded,
		},
		{
			name: "parent timeout > child timeout: child fires first",
			opts: []scope.Option{scope.WithTimeout(5 * time.Second)},
			body: func(s *scope.Scope) error {
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error {
						time.Sleep(3 * time.Second)
						return nil
					})
					return nil
				}, scope.WithTimeout(1*time.Second))
				return nil
			},
			wantErr: context.DeadlineExceeded,
		},
		{
			name: "sibling timeout A < B: A fires first, B observes cancel",
			body: func(s *scope.Scope) error {
				s.Scope(func(a *scope.Scope) error {
					a.Go(func(ctx context.Context) error {
						time.Sleep(3 * time.Second)
						return nil
					})
					return nil
				}, scope.WithTimeout(1*time.Second))
				s.Scope(func(b *scope.Scope) error {
					b.Go(func(ctx context.Context) error {
						assert.ErrorIs(t, ctx.Err(), context.Canceled)
						return ctx.Err()
					})
					return nil
				})
				return nil
			},
			wantErr: context.DeadlineExceeded,
		},
		{
			name: "goroutine error fires before timeout: goroutine error takes priority",
			opts: []scope.Option{scope.WithTimeout(1 * time.Second)},
			body: func(s *scope.Scope) error {
				ch := make(chan struct{})
				s.Go(func(ctx context.Context) error {
					close(ch)
					return assert.AnError
				})
				s.Go(func(ctx context.Context) error {
					<-ch
					time.Sleep(3 * time.Second)
					return nil
				})
				return nil
			},
			wantErr: assert.AnError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				err := scope.Run(t.Context(), tt.body, tt.opts...)

				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
				} else {
					assert.NoError(t, err)
				}
			})
		})
	}

	t.Run("timeout fires with errAggregation: DeadlineExceeded and goroutine errors are aggregated", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			err := scope.Run(context.Background(), func(s *scope.Scope) error {
				s.Go(func(ctx context.Context) error {
					return assert.AnError
				})
				s.Go(func(ctx context.Context) error {
					time.Sleep(3 * time.Second)
					return nil
				})
				return nil
			}, scope.WithTimeout(1*time.Second), scope.WithSupervisor(), scope.WithErrAggregation())

			assert.ErrorIs(t, err, assert.AnError)
			assert.ErrorIs(t, err, context.DeadlineExceeded)
		})
	})
}
