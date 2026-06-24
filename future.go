package scope

import "context"

// Future represents a value that will be produced by a goroutine started with GoFuture.
// The zero value is not usable; obtain one via GoFuture.
type Future[T any] struct {
	ctx     context.Context
	valueCh chan T
}

func newFuture[T any](ctx context.Context) Future[T] {
	return Future[T]{
		ctx:     ctx,
		valueCh: make(chan T, 1),
	}
}

func (f Future[T]) set(value T) {
	f.valueCh <- value
}

// Wait blocks until the goroutine produces a value and returns it, or until ctx
// is canceled, in which case it returns the zero value of T and ctx.Err().
func (f Future[T]) Wait() (T, error) {
	select {
	case v := <-f.valueCh:
		return v, nil
	case <-f.ctx.Done():
		var zero T
		return zero, f.ctx.Err()
	}
}
