package queue

import "time"

type PayloadOption func(*LaravelPayload)

func WithMaxTries(n int) PayloadOption { return func(p *LaravelPayload) { p.MaxTries = &n } }

func WithMaxExceptions(n int) PayloadOption { return func(p *LaravelPayload) { p.MaxExceptions = &n } }

func WithFailOnTimeout(v bool) PayloadOption { return func(p *LaravelPayload) { p.FailOnTimeout = v } }

func WithBackoff(v any) PayloadOption { return func(p *LaravelPayload) { p.Backoff = v } }

func WithTimeout(n int) PayloadOption { return func(p *LaravelPayload) { p.Timeout = &n } }

func WithRetryUntil(t int64) PayloadOption { return func(p *LaravelPayload) { p.RetryUntil = &t } }

func WithTags(tags ...string) PayloadOption { return func(p *LaravelPayload) { p.Tags = tags } }

type WorkerOptions struct {
	Queue       string
	Concurrency int
	MaxJobs     int
	MaxTime     int // seconds
	Sleep       time.Duration
	StopOnEmpty bool
}

type DispatchOptions struct {
	Job           string
	Queue         string
	Delay         time.Duration
	MaxTries      *int
	MaxExceptions *int
	FailOnTimeout bool
	Backoff       any
	Timeout       *int
	RetryUntil    *int64
	Tags          []string
	UniqueFor     int // ShouldBeUnique lock TTL in seconds
	Args          map[string]any
	Chain         []ChainedJob
}

type ChainedJob struct {
	Job  string
	Args map[string]any
}
