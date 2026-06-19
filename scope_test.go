package scope_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/kakkky/scope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		body      func(s *scope.Scope, count *atomic.Int64) error
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var count atomic.Int64
			err := scope.Run(context.Background(), func(s *scope.Scope) error {
				return tt.body(s, &count)
			})
			assert.NoError(t, err)
			assert.Equal(t, tt.wantCount, count.Load())
		})
	}
}

func TestRun_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    func(s *scope.Scope) error
		wantErr error
	}{
		{
			name: "body returns error",
			body: func(s *scope.Scope) error {
				return assert.AnError
			},
			wantErr: assert.AnError,
		},
		{
			name: "body spawns goroutine that returns error",
			body: func(s *scope.Scope) error {
				s.Go(func(ctx context.Context) error {
					return assert.AnError
				})
				return nil
			},
			wantErr: assert.AnError,
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
			wantErr: assert.AnError,
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
			wantErr: assert.AnError,
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
			wantErr: assert.AnError,
		},
		{
			name: "error in child scope body propagates",
			body: func(s *scope.Scope) error {
				s.Scope(func(child *scope.Scope) error {
					return assert.AnError
				})
				return nil
			},
			wantErr: assert.AnError,
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
			wantErr: assert.AnError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := scope.Run(t.Context(), tt.body)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestRun_Error_WithErrAggregation(t *testing.T) {
	t.Parallel()

	var (
		errA = errors.New("error A")
		errB = errors.New("error B")
	)

	tests := []struct {
		name                    string
		body                    func(s *scope.Scope) error
		withAggregationOnRoot   bool // whether to apply WithErrAggregation to the root scope passed to Run
		wantErrs                []error
	}{
		{
			name: "all goroutine errors are collected",
			body: func(s *scope.Scope) error {
				s.Go(func(ctx context.Context) error { return errA })
				s.Go(func(ctx context.Context) error { return errB })
				return nil
			},
			withAggregationOnRoot: true,
			wantErrs:              []error{errA, errB},
		},
		{
			name: "body error is collected",
			body: func(s *scope.Scope) error {
				s.Go(func(ctx context.Context) error { return errA })
				return errB
			},
			withAggregationOnRoot: true,
			wantErrs:              []error{errA, errB},
		},
		{
			name: "goroutine errors in child scope are collected when child also has aggregation",
			body: func(s *scope.Scope) error {
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error { return errA })
					child.Go(func(ctx context.Context) error { return errB })
					return nil
				}, scope.WithErrAggregation())
				return nil
			},
			withAggregationOnRoot: true,
			wantErrs:              []error{errA, errB},
		},
		{
			name: "child scope with aggregation collects errors independently of root scope",
			body: func(s *scope.Scope) error {
				s.Scope(func(child *scope.Scope) error {
					child.Go(func(ctx context.Context) error { return errA })
					child.Go(func(ctx context.Context) error { return errB })
					return nil
				}, scope.WithErrAggregation())
				return nil
			},
			withAggregationOnRoot: false,
			wantErrs:              []error{errA, errB},
		},
		{
			// root uses first-error-wins policy; s.Go fires first, so child's joined error is discarded
			name: "root records first error only when s.Go fires before child scope",
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
			withAggregationOnRoot: false,
			wantErrs:              []error{errA},
		},
		{
			// root uses first-error-wins policy; child scope fires first, so joined error (errA+errB) is recorded
			name: "root records joined child error when child scope fires before s.Go",
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
			withAggregationOnRoot: false,
			wantErrs:              []error{errA, errB},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var opts []scope.Option
			if tt.withAggregationOnRoot {
				opts = append(opts, scope.WithErrAggregation())
			}
			err := scope.Run(t.Context(), tt.body, opts...)
			for _, wantErr := range tt.wantErrs {
				assert.ErrorIs(t, err, wantErr)
			}
		})
	}

	t.Run("errAggregation is not inherited by child scope: only first error propagates", func(t *testing.T) {
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

	t.Run("with supervisor and aggregation, all goroutine errors are collected", func(t *testing.T) {
		t.Parallel()

		errA := errors.New("error A")
		errB := errors.New("error B")
		ch := make(chan struct{})

		err := scope.Run(t.Context(), func(s *scope.Scope) error {
			s.Go(func(ctx context.Context) error {
				close(ch)
				return errA
			})
			s.Go(func(ctx context.Context) error {
				<-ch
				return errB
			})
			return nil
		}, scope.WithSupervisor(), scope.WithErrAggregation())

		for _, wantErr := range []error{errA, errB} {
			assert.ErrorIs(t, err, wantErr)
		}
	})

	t.Run("nested scopes with supervisor and aggregation collect all errors", func(t *testing.T) {
		t.Parallel()

		errA := errors.New("error A")
		errB := errors.New("error B")
		errC := errors.New("error C")

		err := scope.Run(t.Context(), func(s *scope.Scope) error {
			s.Go(func(ctx context.Context) error { return errA })
			s.Scope(func(child *scope.Scope) error {
				child.Go(func(ctx context.Context) error { return errB })
				child.Go(func(ctx context.Context) error { return errC })
				return nil
			}, scope.WithSupervisor(), scope.WithErrAggregation())
			return nil
		}, scope.WithSupervisor(), scope.WithErrAggregation())

		for _, wantErr := range []error{errA, errB, errC} {
			assert.ErrorIs(t, err, wantErr)
		}
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
