package scope

import "context"

// Future represents a value that will be produced by a goroutine started with GoFuture.
// The zero value is not usable; obtain one via GoFuture.
type Future[T any] struct {
	valueCh chan T
}

func newFuture[T any]() Future[T] {
	return Future[T]{
		valueCh: make(chan T, 1),
	}
}

func (r Future[T]) set(value T) {
	r.valueCh <- value
}

// Wait blocks until the goroutine produces a value and returns it, or until ctx
// is canceled, in which case it returns the zero value of T and ctx.Err().
func (r Future[T]) Wait(ctx context.Context) (T, error) {
	select {
	case v := <-r.valueCh:
		return v, nil
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}
