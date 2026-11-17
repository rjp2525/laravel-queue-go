package queue

import "time"

type PayloadOption func(*LaravelPayload)

func WithMaxTries(n int) PayloadOption { return func(p *LaravelPayload) { p.MaxTries = &n } }

func WithMaxExceptions(n int) PayloadOption { return func(p *LaravelPayload) { p.MaxExceptions = &n } }

func WithFailOnTimeout(v bool) PayloadOption { return func(p *LaravelPayload) { p.FailOnTimeout = v } }

// WithBackoff sets the retry delay in seconds: an int, a []int for a
// per-attempt schedule, a time.Duration or []time.Duration, or Laravel's own
// "1,5,10" string. It is written to the payload as a comma-joined string.
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

	// MaxTries is the number of attempts for jobs whose payload has no
	// maxTries, like "queue:work --tries". Zero means unlimited, which is
	// this package's default; Laravel's own default is 1.
	MaxTries int
	// Backoff is the retry delay for jobs whose payload has no backoff, like
	// "queue:work --backoff".
	Backoff time.Duration
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
