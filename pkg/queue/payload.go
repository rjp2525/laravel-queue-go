package queue

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
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

// UnmarshalJSON tolerates the way Redis' cjson re-encodes payloads when a job
// is reserved: an empty JSON array comes back as an empty object, so a job
// pushed with "tags":[] (as Horizon does) is reserved as "tags":{}.
func (p *LaravelPayload) UnmarshalJSON(data []byte) error {
	type plain LaravelPayload
	aux := struct {
		*plain
		Tags json.RawMessage `json:"tags"`
	}{plain: (*plain)(p)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	p.Tags = nil
	if len(aux.Tags) == 0 || aux.Tags[0] != '[' {
		return nil // absent, null, or an empty array mangled into {}
	}
	return json.Unmarshal(aux.Tags, &p.Tags)
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

// MarshalJSON writes "backoff" the way Laravel does: a comma-joined string
// ("10" or "1,5,10") or null. Laravel's worker runs explode(',', ...) on the
// value, which throws on a JSON array.
func (p LaravelPayload) MarshalJSON() ([]byte, error) {
	type plain LaravelPayload
	out := plain(p)
	out.Backoff = normalizeBackoff(p.Backoff)
	return json.Marshal(out)
}

// BackoffSeconds returns the delay before the next retry of a job that has
// just failed its attempt-th run (1-based), like Laravel's
// Worker::calculateBackoff: the attempt-th entry, or the last one when there
// are fewer entries than attempts.
func (p *LaravelPayload) BackoffSeconds(attempt int) int {
	list := backoffList(p.Backoff)
	if len(list) == 0 {
		return 0
	}
	return list[min(max(attempt-1, 0), len(list)-1)]
}

// HasBackoff reports whether the payload carries its own backoff. When it
// does not, the worker's default applies.
func (p *LaravelPayload) HasBackoff() bool {
	return len(backoffList(p.Backoff)) > 0
}

// backoffList reads a backoff value in any of the shapes it can take: the
// comma-joined string Laravel writes, a JSON number or array, or the Go
// values accepted by WithBackoff.
func backoffList(v any) []int {
	switch b := v.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(b) == "" {
			return nil
		}
		parts := strings.Split(b, ",")
		list := make([]int, len(parts))
		for i, part := range parts {
			// Like PHP's (int) cast, anything unparsable counts as 0.
			f, _ := strconv.ParseFloat(strings.TrimSpace(part), 64)
			list[i] = int(f)
		}
		return list
	case int:
		return []int{b}
	case int64:
		return []int{int(b)}
	case float64:
		return []int{int(b)}
	case json.Number:
		f, _ := b.Float64()
		return []int{int(f)}
	case time.Duration:
		return []int{int(b / time.Second)}
	case []int:
		return b
	case []time.Duration:
		list := make([]int, len(b))
		for i, d := range b {
			list[i] = int(d / time.Second)
		}
		return list
	case []any:
		list := make([]int, 0, len(b))
		for _, item := range b {
			list = append(list, backoffList(item)...)
		}
		return list
	}
	return nil
}

// normalizeBackoff converts a backoff value to Laravel's wire format.
func normalizeBackoff(v any) any {
	if s, ok := v.(string); ok {
		return s
	}
	list := backoffList(v)
	if len(list) == 0 {
		return nil
	}
	parts := make([]string, len(list))
	for i, n := range list {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}
