package goutil

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"time"
)

type Server[T any] interface {
	On(req Handler[T]) (*Reply, error)
	GetState() *T
	Context() context.Context
	Close() <-chan struct{}
	Request(req Handler[T]) (result any, err error)
	RequestWithCtx(ctx context.Context, req Handler[T]) (result any, err error)
}

type ServerV1[T any] struct {
	context      context.Context
	cancel       context.CancelFunc
	endCh        chan struct{}
	err          error
	state        *T
	destoryState func(*T)
	reqChan      chan request[T]
}

type Handler[T any] func(*T, Server[T]) (any, error)

type request[T any] struct {
	fn    Handler[T]
	reply *Reply
}

type ServerConfig[T any] struct {
	Ctx            context.Context
	State          *T
	OnCancel       func(state *T)
	RequestChanLen int
}

type Reply struct {
	value any
	err   error
	ch    chan struct{}
}

func (r *Reply) Await(ctx context.Context) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.ch:
		return r.value, r.err
	}
}

func StartServer[T any](config ServerConfig[T]) Server[T] {
	reqChan := make(chan request[T], config.RequestChanLen)
	ctx := config.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	s := ServerV1[T]{
		ctx,
		cancel,
		make(chan struct{}),
		nil,
		config.State,
		config.OnCancel,
		reqChan,
	}
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			s.run()
			time.Sleep(time.Second)
		}
	}()
	return &s
}

func (s *ServerV1[T]) On(req Handler[T]) (*Reply, error) {
	ctx := s.context
	reply := &Reply{
		nil,
		nil,
		make(chan struct{}),
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case s.reqChan <- request[T]{
		req,
		reply,
	}:
		//go on
	}
	return reply, nil
}

func (s *ServerV1[T]) Request(req Handler[T]) (result any, err error) {
	reply, err := s.On(req)
	if err != nil {
		return
	}
	result, err = reply.Await(s.context)
	return
}

func (s *ServerV1[T]) RequestWithCtx(ctx context.Context, req Handler[T]) (result any, err error) {
	reply, err := s.On(req)
	if err != nil {
		return
	}
	result, err = reply.Await(ctx)
	return
}

func (s *ServerV1[T]) Close() <-chan struct{} {
	s.cancel()
	return s.endCh
}

func (s *ServerV1[T]) Context() context.Context {
	return s.context
}

func (s *ServerV1[T]) GetState() *T {
	return s.state
}

func (s *ServerV1[T]) run() (err error) {
	defer s.cancel()
	defer func() {
		s.err = err
		close(s.endCh)
	}()
	var currentReply *Reply
	defer func() {
		if r := recover(); r != nil {
			// log.Printf("recovered, %T, %v\n", s.GetState(), r)
			log.Printf("recovered, %T, %v\n%s\n", s.GetState(), r, string(debug.Stack()))
			err = fmt.Errorf("%w: %v", errors.New("internal error"), r)
		}
		if currentReply != nil {
			currentReply.err = err
			close(currentReply.ch)
			currentReply = nil
		}
	}()
	defer func() {
		if s.destoryState == nil {
			return
		}
		s.destoryState(s.state)
	}()
	ctx := s.context
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case req := <-s.reqChan:
			currentReply = req.reply
			currentReply.value, currentReply.err = req.fn(s.state, s)
			close(currentReply.ch)
			currentReply = nil
		}
	}
}
