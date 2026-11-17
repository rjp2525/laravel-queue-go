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

// WithDelay records the dispatch delay in the payload's "delay" key, as
// Laravel 12+ does. It does not delay the job by itself; Dispatcher.Later and
// DispatchOptions.Delay set it for you.
func WithDelay(d time.Duration) PayloadOption {
	return func(p *LaravelPayload) { p.Delay = new(int(d / time.Second)) }
}

// WithLaravelVersion selects the payload format to write. See LaravelVersion.
func WithLaravelVersion(v LaravelVersion) PayloadOption {
	return func(p *LaravelPayload) { p.version = v }
}

// WithBatchID sets data.batchId (Laravel 12+).
func WithBatchID(id string) PayloadOption {
	return func(p *LaravelPayload) { p.Data.BatchID = &id }
}

// WithDeleteWhenMissingModels sets the Laravel 13 payload flag that makes the
// PHP worker drop the job, instead of failing it, when a serialized model no
// longer exists.
func WithDeleteWhenMissingModels(v bool) PayloadOption {
	return func(p *LaravelPayload) { p.DeleteWhenMissingModels = v }
}

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
