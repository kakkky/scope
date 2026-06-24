package scope

import "context"

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

func (r Future[T]) Wait(ctx context.Context) (T, error) {
	select {
	case v := <-r.valueCh:
		return v, nil
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}
