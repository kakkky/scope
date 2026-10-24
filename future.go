package scope

// Future represents a value that will be produced by a goroutine started with Scope.GoFuture.
// The zero value is not usable; obtain one via Scope.GoFuture.
type Future[T any] struct {
	r *result[T]
}

// result is written exactly once before done is closed, so readers that
// receive from done observe val and err without further synchronization.
type result[T any] struct {
	done chan struct{}
	val  T
	err  error
}

func newFuture[T any]() Future[T] {
	return Future[T]{r: &result[T]{done: make(chan struct{})}}
}

func (f Future[T]) resolve(val T, err error) {
	f.r.val, f.r.err = val, err
	close(f.r.done)
}

// Wait blocks until the goroutine started by Scope.GoFuture has finished and
// returns the value and error returned by fn.
//
// If fn panics, Wait returns the zero value of T and an error describing the panic.
// If fn was never started because the scope's context was canceled while
// waiting for a concurrency slot (see WithMaxConcurrency), Wait returns the
// zero value of T and the cancellation cause of the scope.
//
// Wait may be called any number of times, from any goroutine, and also after
// Run has returned; every call returns the same result.
func (f Future[T]) Wait() (T, error) {
	<-f.r.done
	return f.r.val, f.r.err
}
