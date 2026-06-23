package scope

import "context"

type Result[T any] struct {
	valueCh chan T
}

func NewResult[T any]() Result[T] {
	return Result[T]{valueCh: make(chan T, 1)}
}

func (res Result[T]) Set(value T) {
	res.valueCh <- value
}

func (res Result[T]) Wait(ctx context.Context) (T, error) {
	select {
	case value := <-res.valueCh:
		return value, nil
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}
