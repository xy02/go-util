package goutil

import (
	"context"
	"errors"
	"log"
	"runtime/debug"
	"time"
)

type Server[T any] interface {
	On(req Handler[T]) (*Reply, error)
	GetState() *T
	Context() context.Context
}

type ServerV1[T any] struct {
	context      context.Context
	state        *T
	destoryState func(*T)
	reqChan      chan request[T]
}

type Handler[T any] func(context.Context, *T) (any, error)

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
	s := ServerV1[T]{
		ctx,
		config.State,
		config.OnCancel,
		reqChan,
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

func (s *ServerV1[T]) Context() context.Context {
	return s.context
}

func (s *ServerV1[T]) GetState() *T {
	return s.state
}

func (s *ServerV1[T]) run(ctx context.Context) (err error) {
	var currentReply *Reply
	defer func() {
		if r := recover(); r != nil {
			// log.Printf("recovered, %T, %v\n", s.GetState(), r)
			log.Printf("recovered, %T, %v\n%s\n", s.GetState(), r, string(debug.Stack()))
		}
		if currentReply != nil {
			currentReply.err = errors.New("internal error")
			close(currentReply.ch)
		}
	}()
	defer func() {
		if s.destoryState == nil {
			return
		}
		s.destoryState(s.state)
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case req := <-s.reqChan:
			currentReply = req.reply
			currentReply.value, currentReply.err = req.fn(ctx, s.state)
			close(currentReply.ch)
		}
	}
}
