package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rjp2525/laravel-queue-go/pkg/middleware"
	"github.com/rjp2525/laravel-queue-go/pkg/phpserialize"
)

var _ middleware.Job = (*Job)(nil)

type CommandFormat int

const (
	FormatPHP  CommandFormat = iota // PHP serialize() output
	FormatJSON                      // JSON object
)

type Job struct {
	raw     *RawJob
	payload *LaravelPayload
	data    *phpserialize.Object // normalized property bag (works for both PHP and JSON sources)
	format  CommandFormat
	driver  Driver
	ctx     context.Context
}

func ParseJob(ctx context.Context, raw *RawJob, driver Driver) (*Job, error) {
	var payload LaravelPayload
	if err := json.Unmarshal([]byte(raw.Body), &payload); err != nil {
		return nil, fmt.Errorf("parse job payload: %w", err)
	}

	data, format := decodeCommand(payload.Data.Command)

	return &Job{
		raw:     raw,
		payload: &payload,
		data:    data,
		format:  format,
		driver:  driver,
		ctx:     ctx,
	}, nil
}

// decodeCommand tries PHP unserialize first, then JSON. Returns the decoded
// properties as a Object (used as a uniform property bag) and the format detected.
func decodeCommand(command string) (*phpserialize.Object, CommandFormat) {
	if command == "" {
		return nil, FormatPHP
	}

	// Use first-byte heuristic to avoid wasted decode attempts.
	// PHP serialized objects start with 'O:', JSON objects start with '{'.
	if command[0] == '{' {
		return decodeCommandJSON(command)
	}
	decoded, err := phpserialize.Decode(command)
	if err == nil {
		if obj, ok := decoded.(*phpserialize.Object); ok {
			return obj, FormatPHP
		}
	}
	// PHP failed — fall back to JSON in case the heuristic was wrong.
	return decodeCommandJSON(command)
}

func decodeCommandJSON(command string) (*phpserialize.Object, CommandFormat) {
	var props map[string]any
	if err := json.Unmarshal([]byte(command), &props); err == nil && len(props) > 0 {
		return &phpserialize.Object{
			ClassName:  stringFromAny(props["commandName"]),
			Properties: props,
		}, FormatJSON
	}
	return nil, FormatPHP
}

func (j *Job) Format() CommandFormat { return j.format }

// Property accessors — work identically regardless of command format.

func (j *Job) GetString(key string) string {
	if j.data == nil {
		return ""
	}
	return phpserialize.GetString(j.data, key)
}

func (j *Job) GetInt(key string) int64 {
	if j.data == nil {
		return 0
	}
	// JSON numbers unmarshal as float64 by default.
	if j.format == FormatJSON {
		v := j.data.Properties[key]
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case json.Number:
			i, _ := n.Int64()
			return i
		}
		return 0
	}
	return phpserialize.GetInt(j.data, key)
}

func (j *Job) GetFloat(key string) float64 {
	if j.data == nil {
		return 0
	}
	return phpserialize.GetFloat(j.data, key)
}

func (j *Job) GetBool(key string) bool {
	if j.data == nil {
		return false
	}
	return phpserialize.GetBool(j.data, key)
}

func (j *Job) GetSlice(key string) []any {
	if j.data == nil {
		return nil
	}
	// JSON arrays are already []any.
	if j.format == FormatJSON {
		if s, ok := j.data.Properties[key].([]any); ok {
			return s
		}
		return nil
	}
	return phpserialize.GetSlice(j.data, key)
}

func (j *Job) GetMap(key string) map[string]any {
	if j.data == nil {
		return nil
	}
	// JSON objects are already map[string]any.
	if j.format == FormatJSON {
		if m, ok := j.data.Properties[key].(map[string]any); ok {
			return m
		}
		return nil
	}
	return phpserialize.GetMap(j.data, key)
}

func (j *Job) GetModelID(key string) *phpserialize.ModelIdentifier {
	if j.data == nil {
		return nil
	}
	v, ok := j.data.Properties[key]
	if !ok {
		return nil
	}
	// PHP-serialized ModelIdentifier.
	if mid := phpserialize.AsModelIdentifier(v); mid != nil {
		return mid
	}
	// JSON-serialized ModelIdentifier (nested object).
	if m, ok := v.(map[string]any); ok {
		mid := &phpserialize.ModelIdentifier{
			Class:      stringFromAny(m["class"]),
			ID:         m["id"],
			Connection: stringFromAny(m["connection"]),
		}
		if cc, ok := m["collectionClass"].(string); ok {
			mid.CollectionClass = &cc
		}
		if rels, ok := m["relations"].([]any); ok {
			for _, r := range rels {
				if s, ok := r.(string); ok {
					mid.Relations = append(mid.Relations, s)
				}
			}
		}
		return mid
	}
	return nil
}

func stringFromAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// Metadata accessors.

func (j *Job) UUID() string               { return j.payload.UUID }
func (j *Job) DisplayName() string        { return j.payload.DisplayName }
func (j *Job) CommandName() string        { return j.payload.Data.CommandName }
func (j *Job) Attempts() int              { return j.payload.Attempts }
func (j *Job) Queue() string              { return j.raw.Queue }
func (j *Job) MaxTries() *int             { return j.payload.MaxTries }
func (j *Job) MaxExceptions() *int        { return j.payload.MaxExceptions }
func (j *Job) Timeout() *int              { return j.payload.Timeout }
func (j *Job) Tags() []string             { return j.payload.Tags }
func (j *Job) Payload() *LaravelPayload   { return j.payload }
func (j *Job) Raw() *RawJob               { return j.raw }
func (j *Job) Data() *phpserialize.Object { return j.data }

// Lifecycle methods.

func (j *Job) Delete() error {
	return j.driver.Delete(j.ctx, j.raw)
}

func (j *Job) Release(delay time.Duration) error {
	j.payload.Attempts++
	body, err := json.Marshal(j.payload)
	if err != nil {
		return fmt.Errorf("marshal payload for release: %w", err)
	}
	j.raw.Body = string(body)
	return j.driver.Release(j.ctx, j.raw, delay)
}

func (j *Job) HasFailed() bool {
	if j.payload.MaxTries != nil && j.payload.Attempts >= *j.payload.MaxTries {
		return true
	}
	if j.payload.RetryUntil != nil && time.Now().Unix() >= *j.payload.RetryUntil {
		return true
	}
	return false
}

func (j *Job) BackoffDuration() time.Duration {
	secs := j.payload.BackoffSeconds(j.payload.Attempts)
	return time.Duration(secs) * time.Second
}

func (j *Job) ChainedJobs() []string {
	if j.data == nil {
		return nil
	}
	chained := j.data.Properties["chained"]
	// PHP format: Array of serialized payloads.
	if arr, ok := chained.(*phpserialize.Array); ok {
		result := make([]string, 0, len(arr.Values))
		for _, v := range arr.Values {
			if s, ok := v.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	// JSON format: []any of strings.
	if arr, ok := chained.([]any); ok {
		result := make([]string, 0, len(arr))
		for _, v := range arr {
			if s, ok := v.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

func (j *Job) BatchID() string {
	if j.data == nil {
		return ""
	}
	if v, ok := j.data.Properties["batchId"].(string); ok {
		return v
	}
	return ""
}
