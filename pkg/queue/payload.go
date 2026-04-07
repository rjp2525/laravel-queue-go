package queue

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	// LaravelCallQueuedHandler is the default job handler FQCN used by Laravel.
	LaravelCallQueuedHandler = `Illuminate\Queue\CallQueuedHandler@call`

	DefaultQueueName = "default"

	DefaultConnection = "redis"

	// DefaultRedisPrefix is the default Redis key prefix used by Laravel.
	// Laravel generates this from: Str::slug(env('APP_NAME', 'laravel')).'-database-'
	// Users with a custom APP_NAME should set their prefix accordingly.
	DefaultRedisPrefix = "laravel-database-"
)

type LaravelPayload struct {
	UUID          string      `json:"uuid"`
	DisplayName   string      `json:"displayName"`
	Job           string      `json:"job"`
	MaxTries      *int        `json:"maxTries"`
	MaxExceptions *int        `json:"maxExceptions"`
	FailOnTimeout bool        `json:"failOnTimeout"`
	Backoff       any         `json:"backoff"`
	Timeout       *int        `json:"timeout"`
	RetryUntil    *int64      `json:"retryUntil"`
	Tags          []string    `json:"tags"`
	Data          PayloadData `json:"data"`
	ID            string      `json:"id"`
	Attempts      int         `json:"attempts"`
	PushedAt      float64     `json:"pushedAt"`
}

type PayloadData struct {
	CommandName string `json:"commandName"`
	Command     string `json:"command"`
}

func NewPayload(jobName string, command string, opts ...PayloadOption) *LaravelPayload {
	p := &LaravelPayload{
		UUID:        uuid.NewString(),
		DisplayName: jobName,
		Job:         LaravelCallQueuedHandler,
		Data: PayloadData{
			CommandName: jobName,
			Command:     command,
		},
		ID:       generateID(),
		Attempts: 0,
		PushedAt: float64(time.Now().UnixMicro()) / 1e6,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (p *LaravelPayload) BackoffSeconds(attempt int) int {
	switch v := p.Backoff.(type) {
	case nil:
		return 0
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case int:
		return v
	case []any:
		if len(v) == 0 {
			return 0
		}
		idx := attempt - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(v) {
			idx = len(v) - 1
		}
		switch n := v[idx].(type) {
		case float64:
			return int(n)
		case json.Number:
			i, _ := n.Int64()
			return int(i)
		case int:
			return n
		}
	case []int:
		if len(v) == 0 {
			return 0
		}
		idx := attempt - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(v) {
			idx = len(v) - 1
		}
		return v[idx]
	}
	return 0
}
