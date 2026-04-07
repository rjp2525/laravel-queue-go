package middleware

import (
	"context"
	"time"
)

// Job is the interface that middleware uses to inspect jobs.
// Implemented by *queue.Job. Defined here to avoid circular imports.
type Job interface {
	UUID() string
	DisplayName() string
	CommandName() string
	Attempts() int
	Queue() string
	MaxTries() *int
	Timeout() *int
	Tags() []string
	BatchID() string
}

// Middleware wraps handler execution. Call next() to proceed.
type Middleware func(ctx context.Context, job Job, next func() error) error

type TimeoutGetter func(job Job) *time.Duration

type KeyFunc func(job Job) string

type Pipeline struct {
	middleware []Middleware
}

func NewPipeline(middleware ...Middleware) *Pipeline {
	return &Pipeline{middleware: middleware}
}

func (p *Pipeline) Use(m ...Middleware) {
	p.middleware = append(p.middleware, m...)
}

func (p *Pipeline) Then(ctx context.Context, job Job, handler func() error) error {
	if len(p.middleware) == 0 {
		return handler()
	}
	return p.execute(ctx, job, handler, 0)
}

func (p *Pipeline) execute(ctx context.Context, job Job, handler func() error, index int) error {
	if index >= len(p.middleware) {
		return handler()
	}
	return p.middleware[index](ctx, job, func() error {
		return p.execute(ctx, job, handler, index+1)
	})
}
