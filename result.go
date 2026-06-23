package scope

import "context"

type Result[T any] struct {
	valueCh chan T
}

func newResult[T any]() Result[T] {
	return Result[T]{
		valueCh: make(chan T, 1),
	}
}

func (r Result[T]) set(value T) {
	r.valueCh <- value
}

func (r Result[T]) Wait(ctx context.Context) (T, error) {
	select {
	case v := <-r.valueCh:
		return v, nil
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}
