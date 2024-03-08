package goutil

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
)

type Coroutine[T any] struct {
	cancel context.CancelFunc
	endCh  chan struct{}
	value  *T
	err    error
}

func Go[T any](ctx context.Context, fn func(context.Context) (T, error)) *Coroutine[T] {
	ctx, cancel := context.WithCancel(ctx)
	endCh := make(chan struct{})
	c := &Coroutine[T]{
		cancel,
		endCh,
		nil,
		nil,
	}
	go func() {
		defer close(endCh)
		defer func() {
			if r := recover(); r != nil {
				// log.Printf("recovered, %T, %v\n", s.GetState(), r)
				log.Printf("recovered, %v\n%s\n", r, string(debug.Stack()))
				c.err = fmt.Errorf("%w: %v", errors.New("internal error"), r)
			}
		}()
		result, err := fn(ctx)
		c.value = &result
		c.err = err
	}()
	return c
}

func (c *Coroutine[T]) Cancel() {
	c.cancel()
}

func (c *Coroutine[T]) Await(ctx context.Context) (result T, err error) {
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	case <-c.endCh:
		result = *c.value
		err = c.err
		return
	}
}

func (c *Coroutine[T]) EndChan() <-chan struct{} {
	return c.endCh
}
