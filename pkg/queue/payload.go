package queue

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

	// DefaultRedisPrefix is the Redis key prefix of a fresh Laravel 12 or 13
	// application named "Laravel": Str::slug(env('APP_NAME', 'laravel')).'-database-'.
	// Laravel 10 and 11 use "laravel_database_"; see LaravelVersion.RedisPrefix.
	DefaultRedisPrefix = "laravel-database-"
)

type LaravelPayload struct {
	UUID          string `json:"uuid"`
	DisplayName   string `json:"displayName"`
	Job           string `json:"job"`
	MaxTries      *int   `json:"maxTries"`
	MaxExceptions *int   `json:"maxExceptions"`
	FailOnTimeout bool   `json:"failOnTimeout"`
	// CountCrashesAsExceptions is written by Laravel 13.
	CountCrashesAsExceptions bool   `json:"countCrashesAsExceptions"`
	Backoff                  any    `json:"backoff"`
	Timeout                  *int   `json:"timeout"`
	RetryUntil               *int64 `json:"retryUntil"`
	// DeleteWhenMissingModels is written by Laravel 13, whose worker reads it
	// from the payload instead of reflecting on the job class.
	DeleteWhenMissingModels bool        `json:"deleteWhenMissingModels"`
	Tags                    []string    `json:"tags"`
	Data                    PayloadData `json:"data"`
	// CreatedAt is the unix time the job was created. Written by Laravel 12+.
	CreatedAt *int64 `json:"createdAt"`
	// LogContext is Laravel 11+'s dehydrated Context ("illuminate:log:context").
	LogContext json.RawMessage `json:"illuminate:log:context"`
	ID         string          `json:"id"`
	Attempts   int             `json:"attempts"`
	PushedAt   float64         `json:"pushedAt"`
	// Delay is the delay in seconds the job was dispatched with. Written by
	// Laravel 12+. Every Laravel version's worker falls back to it as the
	// retry backoff when the payload has no backoff of its own.
	Delay *int `json:"delay"`

	// version selects the keys MarshalJSON writes.
	version LaravelVersion
}

type PayloadData struct {
	CommandName string `json:"commandName"`
	Command     string `json:"command"`
	// BatchID is written by Laravel 12+. The authoritative batch id is the
	// batchId property inside Command.
	BatchID *string `json:"batchId"`
}

// LaravelVersion returns the version whose payload format MarshalJSON
// writes. Payloads decoded from JSON report LaravelAuto.
func (p *LaravelPayload) LaravelVersion() LaravelVersion { return p.version }

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
	now := time.Now()
	p := &LaravelPayload{
		UUID:        uuid.NewString(),
		DisplayName: jobName,
		Job:         LaravelCallQueuedHandler,
		Data: PayloadData{
			CommandName: jobName,
			Command:     command,
		},
		ID:        generateID(),
		Attempts:  0,
		PushedAt:  float64(now.UnixMicro()) / 1e6,
		CreatedAt: new(now.Unix()),
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

// MarshalJSON writes the payload with the keys, and in the key order, of the
// Laravel version it was built for (see WithLaravelVersion); LaravelAuto
// writes the latest format. Keys a version does not know are left out.
//
// "backoff" is written the way Laravel does: a comma-joined string ("10" or
// "1,5,10") or null. Laravel's worker runs explode(',', ...) on the value,
// which throws on a JSON array.
func (p LaravelPayload) MarshalJSON() ([]byte, error) {
	v := p.version.orLatest()

	var w objectWriter
	w.field("uuid", p.UUID)
	w.field("displayName", p.DisplayName)
	w.field("job", p.Job)
	w.field("maxTries", p.MaxTries)
	w.field("maxExceptions", p.MaxExceptions)
	w.field("failOnTimeout", p.FailOnTimeout)
	if v >= Laravel13 {
		w.field("countCrashesAsExceptions", p.CountCrashesAsExceptions)
	}
	w.field("backoff", normalizeBackoff(p.Backoff))
	w.field("timeout", p.Timeout)
	w.field("retryUntil", p.RetryUntil)
	if v >= Laravel13 {
		w.field("deleteWhenMissingModels", p.DeleteWhenMissingModels)
	}

	var data objectWriter
	data.field("commandName", p.Data.CommandName)
	data.field("command", p.Data.Command)
	if v >= Laravel12 {
		data.field("batchId", p.Data.BatchID)
	}
	dataJSON, err := data.bytes()
	if err != nil {
		return nil, err
	}
	w.field("data", json.RawMessage(dataJSON))

	if v >= Laravel12 && p.CreatedAt != nil {
		w.field("createdAt", p.CreatedAt)
	}
	if v >= Laravel11 && len(p.LogContext) > 0 {
		w.field("illuminate:log:context", p.LogContext)
	}
	// Not part of Laravel's own payload: Horizon adds these, and this package
	// has always written them.
	w.field("tags", p.Tags)
	w.field("pushedAt", p.PushedAt)

	w.field("id", p.ID)
	w.field("attempts", p.Attempts)
	if v >= Laravel12 {
		w.field("delay", p.Delay)
	}
	return w.bytes()
}

// objectWriter builds a JSON object with a fixed key order.
type objectWriter struct {
	buf bytes.Buffer
	err error
}

func (w *objectWriter) field(name string, value any) {
	if w.err != nil {
		return
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		w.err = fmt.Errorf("payload key %q: %w", name, err)
		return
	}
	if w.buf.Len() == 0 {
		w.buf.WriteByte('{')
	} else {
		w.buf.WriteByte(',')
	}
	w.buf.WriteString(strconv.Quote(name)) // key names are plain ASCII
	w.buf.WriteByte(':')
	w.buf.Write(encoded)
}

func (w *objectWriter) bytes() ([]byte, error) {
	if w.err != nil {
		return nil, w.err
	}
	if w.buf.Len() == 0 {
		return []byte("{}"), nil
	}
	w.buf.WriteByte('}')
	return w.buf.Bytes(), nil
}

// BackoffSeconds returns the delay before the next retry of a job that has
// just failed its attempt-th run (1-based), like Laravel's
// Worker::calculateBackoff: the attempt-th entry, or the last one when there
// are fewer entries than attempts.
func (p *LaravelPayload) BackoffSeconds(attempt int) int {
	list := p.backoffSchedule()
	if len(list) == 0 {
		return 0
	}
	return list[min(max(attempt-1, 0), len(list)-1)]
}

// backoffSchedule is Laravel's Job::backoff(): the payload's backoff, or the
// delay it was dispatched with when it has none.
func (p *LaravelPayload) backoffSchedule() []int {
	if p.Backoff == nil && p.Delay != nil {
		return []int{*p.Delay}
	}
	return backoffList(p.Backoff)
}

// HasBackoff reports whether the payload carries its own backoff. When it
// does not, the worker's default applies.
func (p *LaravelPayload) HasBackoff() bool {
	return len(p.backoffSchedule()) > 0
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
