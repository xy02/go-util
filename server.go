package goutil

import (
	"context"
	"errors"
	"log"
	"runtime/debug"
	"time"

	"github.com/chebyrash/promise"
	"github.com/sourcegraph/conc/pool"
)

type Server[T any] interface {
	getPool() promise.Pool
	On(req Handler[T]) (*promise.Promise[any], error)
	Send(req Handler[T]) *promise.Promise[any]
	GetState() *T
	Context() context.Context
}

type request[T any] struct {
	replyChan chan<- Result
	fn        Handler[T]
}

type Result struct {
	Ok  any
	Err error
}

func (s *ServerV1[T]) Send(req Handler[T]) *promise.Promise[any] {
	p, err := s.On(req)
	if err != nil {
		return promise.NewWithPool(func(resolve func(any), reject func(error)) {
			reject(errors.New("ctx done"))
		}, s.getPool())
	}
	return p
}

func (s *ServerV1[T]) On(req Handler[T]) (*promise.Promise[any], error) {
	ctx := s.Context()
	replyChan := make(chan Result, 1)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case s.reqChan <- request[T]{
		replyChan,
		req,
	}:
		//go on
	}

	// promise.NewWithPool()
	return promise.NewWithPool(func(resolve func(any), reject func(error)) {
		reply := <-replyChan
		if reply.Err != nil {
			reject(reply.Err)
			return
		}
		p, ok := reply.Ok.(*promise.Promise[any])
		if !ok {
			resolve(reply.Ok)
			return
		}
		r, err := p.Await(ctx)
		if err != nil {
			reject(err)
			return
		}
		resolve(*r)
	}, s.getPool()), nil
}

// type Request[T any] interface {
// 	Handle(Server[T]) Result
// }

type Handler[T any] func(Server[T]) Result

type ServerV1[T any] struct {
	context      context.Context
	state        *T
	destoryState func(*T)
	reqChan      chan request[T]
	p            promise.Pool
}

func StartServer[T any](ctx context.Context, state *T, destoryState func(*T)) Server[T] {
	p := pool.New().WithMaxGoroutines(4)
	reqChan := make(chan request[T], 1000)
	s := ServerV1[T]{
		ctx,
		state,
		destoryState,
		reqChan,
		promise.FromConcPool(p),
	}
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			s.run(ctx)
			time.Sleep(time.Second)
		}
	}()
	return &s
}

func (s *ServerV1[T]) getPool() promise.Pool {
	return s.p
}

func (s *ServerV1[T]) run(ctx context.Context) (err error) {
	var currentReq request[T]
	defer func() {
		if r := recover(); r != nil {
			// log.Printf("recovered, %T, %v\n", s.GetState(), r)
			log.Printf("recovered, %T, %v\n%s\n", s.GetState(), r, string(debug.Stack()))
		}
		currentReq.replyChan <- Result{
			Err: errors.New("interval error"),
		}
	}()
	defer s.destoryState(s.state)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case req := <-s.reqChan:
			currentReq = req
			result := req.fn(s)
			if result.Err != nil {
				log.Println(result.Err)
			}
			req.replyChan <- result
		}
	}
}

//	func (s *ServerV1[T]) Do(ctx context.Context, req Request[T]) (err error) {
//		select {
//		case <-ctx.Done():
//			return ctx.Err()
//		case s.reqChan <- req:
//			return nil
//		}
//	}
//
//	func (s *ServerV1[T]) send(req request[T]) {
//		s.reqChan <- req
//	}
func (s *ServerV1[T]) Context() context.Context {
	return s.context
}

func (s *ServerV1[T]) GetState() *T {
	return s.state
}
