package scope_test

import (
	"context"
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var count atomic.Int64
			err := scope.Run(context.Background(), func(s *scope.Scope) error {
				return tt.body(s, &count)
			})
			assert.NoError(t, err)
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := scope.Run(context.Background(), tt.body)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
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

func TestRun_Panic(t *testing.T) {
	t.Parallel()

	t.Run("panic is recovered as error", func(t *testing.T) {
		t.Parallel()

		err := scope.Run(context.Background(), func(s *scope.Scope) error {
			s.Go(func(ctx context.Context) error {
				panic("boom")
			})
			return nil
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "boom", "panic value should be in error message")
		assert.Contains(t, err.Error(), "goroutine", "stack trace should be in error message")
	})

	t.Run("panic cancels running children", func(t *testing.T) {
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
		assert.True(t, observed.Load(), "sibling did not observe cancel after panic")
	})
}

func TestRun_GoAfterReturned(t *testing.T) {
	t.Parallel()

	t.Run("Go panics when called after Run returned", func(t *testing.T) {
		t.Parallel()

		var captured *scope.Scope
		err := scope.Run(t.Context(), func(s *scope.Scope) error {
			captured = s
			return nil
		})
		assert.NoError(t, err)
		assert.PanicsWithValue(t,
			"scope: misuse: Go called outside scope lifetime",
			func() {
				captured.Go(func(ctx context.Context) error { return nil })
			},
		)
	})
}
