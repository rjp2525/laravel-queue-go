package queue

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// LaravelPayload JSON round-trip
// ---------------------------------------------------------------------------

func TestPayloadMarshalUnmarshalRoundTrip(t *testing.T) {
	maxTries := 3
	timeout := 30
	p := &LaravelPayload{
		UUID:          "test-uuid",
		DisplayName:   `App\Jobs\Test`,
		Job:           `Illuminate\Queue\CallQueuedHandler@call`,
		MaxTries:      &maxTries,
		MaxExceptions: nil,
		FailOnTimeout: false,
		Backoff:       []any{float64(5), float64(30)},
		Timeout:       &timeout,
		RetryUntil:    nil,
		Tags:          []string{"tag1"},
		Data: PayloadData{
			CommandName: `App\Jobs\Test`,
			Command:     `O:14:"App\Jobs\Test":0:{}`,
		},
		ID:       "abc123",
		Attempts: 0,
		PushedAt: 1711900000.123,
	}

	data, err := json.Marshal(p)
	require.NoError(t, err)

	var p2 LaravelPayload
	err = json.Unmarshal(data, &p2)
	require.NoError(t, err)

	assert.Equal(t, p.UUID, p2.UUID)
	assert.Equal(t, p.DisplayName, p2.DisplayName)
	assert.Equal(t, p.Job, p2.Job)
	assert.Equal(t, *p.MaxTries, *p2.MaxTries)
	assert.Nil(t, p2.MaxExceptions)
	assert.Equal(t, p.FailOnTimeout, p2.FailOnTimeout)
	assert.Equal(t, *p.Timeout, *p2.Timeout)
	assert.Equal(t, p.Tags, p2.Tags)
	assert.Equal(t, p.Data.CommandName, p2.Data.CommandName)
	assert.Equal(t, p.Data.Command, p2.Data.Command)
	assert.Equal(t, p.ID, p2.ID)
	assert.Equal(t, p.Attempts, p2.Attempts)
	assert.InDelta(t, p.PushedAt, p2.PushedAt, 1e-3)
}

// ---------------------------------------------------------------------------
// BackoffSeconds
// ---------------------------------------------------------------------------

func TestBackoffSecondsNil(t *testing.T) {
	p := &LaravelPayload{Backoff: nil}
	assert.Equal(t, 0, p.BackoffSeconds(1))
}

func TestBackoffSecondsSingleInt(t *testing.T) {
	p := &LaravelPayload{Backoff: float64(10)}
	assert.Equal(t, 10, p.BackoffSeconds(1))
	assert.Equal(t, 10, p.BackoffSeconds(3))
}

func TestBackoffSecondsProgressiveArray(t *testing.T) {
	p := &LaravelPayload{Backoff: []any{float64(5), float64(30), float64(90)}}
	assert.Equal(t, 5, p.BackoffSeconds(1))
	assert.Equal(t, 30, p.BackoffSeconds(2))
	assert.Equal(t, 90, p.BackoffSeconds(3))
	// Beyond last element uses last.
	assert.Equal(t, 90, p.BackoffSeconds(10))
}

func TestBackoffSecondsZeroAttempt(t *testing.T) {
	p := &LaravelPayload{Backoff: []any{float64(5), float64(30)}}
	assert.Equal(t, 5, p.BackoffSeconds(0))
}

func TestBackoffSecondsEmptyArray(t *testing.T) {
	p := &LaravelPayload{Backoff: []any{}}
	assert.Equal(t, 0, p.BackoffSeconds(1))
}

func TestBackoffSecondsIntSlice(t *testing.T) {
	p := &LaravelPayload{Backoff: []int{10, 20}}
	assert.Equal(t, 10, p.BackoffSeconds(1))
	assert.Equal(t, 20, p.BackoffSeconds(2))
	assert.Equal(t, 20, p.BackoffSeconds(5))
}

// ---------------------------------------------------------------------------
// Keys
// ---------------------------------------------------------------------------

func TestKeysReady(t *testing.T) {
	k := NewKeys("laravel-database-")
	assert.Equal(t, "laravel-database-queues:default", k.Ready("default"))
}

func TestKeysDelayed(t *testing.T) {
	k := NewKeys("laravel-database-")
	assert.Equal(t, "laravel-database-queues:default:delayed", k.Delayed("default"))
}

func TestKeysReserved(t *testing.T) {
	k := NewKeys("laravel-database-")
	assert.Equal(t, "laravel-database-queues:default:reserved", k.Reserved("default"))
}

func TestKeysNotify(t *testing.T) {
	k := NewKeys("laravel-database-")
	assert.Equal(t, "laravel-database-queues:default:notify", k.Notify("default"))
}

func TestKeysCustomPrefix(t *testing.T) {
	k := NewKeys("myapp_")
	assert.Equal(t, "myapp_queues:emails", k.Ready("emails"))
	assert.Equal(t, "myapp_queues:emails:delayed", k.Delayed("emails"))
}

func TestKeysEmptyPrefix(t *testing.T) {
	k := NewKeys("")
	assert.Equal(t, "queues:default", k.Ready("default"))
}

// ---------------------------------------------------------------------------
// NewPayload
// ---------------------------------------------------------------------------

func TestNewPayloadDefaults(t *testing.T) {
	p := NewPayload(`App\Jobs\Foo`, `O:15:"App\Jobs\Foo":0:{}`)
	assert.NotEmpty(t, p.UUID)
	assert.NotEmpty(t, p.ID)
	assert.Equal(t, `App\Jobs\Foo`, p.DisplayName)
	assert.Equal(t, `Illuminate\Queue\CallQueuedHandler@call`, p.Job)
	assert.Equal(t, 0, p.Attempts)
	assert.Nil(t, p.MaxTries)
	assert.Nil(t, p.Timeout)
	assert.Equal(t, `App\Jobs\Foo`, p.Data.CommandName)
	assert.True(t, p.PushedAt > 0)
}

func TestNewPayloadWithOptions(t *testing.T) {
	p := NewPayload(`App\Jobs\Bar`, "command",
		WithMaxTries(5),
		WithTimeout(60),
		WithTags("a", "b"),
		WithFailOnTimeout(true),
		WithBackoff([]int{10, 20}),
	)
	require.NotNil(t, p.MaxTries)
	assert.Equal(t, 5, *p.MaxTries)
	require.NotNil(t, p.Timeout)
	assert.Equal(t, 60, *p.Timeout)
	assert.Equal(t, []string{"a", "b"}, p.Tags)
	assert.True(t, p.FailOnTimeout)
	assert.Equal(t, []int{10, 20}, p.Backoff)
}

func TestNewPayloadWithRetryUntil(t *testing.T) {
	ts := int64(1711990000)
	p := NewPayload("Job", "cmd", WithRetryUntil(ts))
	require.NotNil(t, p.RetryUntil)
	assert.Equal(t, ts, *p.RetryUntil)
}

func TestNewPayloadWithMaxExceptions(t *testing.T) {
	p := NewPayload("Job", "cmd", WithMaxExceptions(2))
	require.NotNil(t, p.MaxExceptions)
	assert.Equal(t, 2, *p.MaxExceptions)
}

// ---------------------------------------------------------------------------
// generateID
// ---------------------------------------------------------------------------

func Test_generateID_format(t *testing.T) {
	id := generateID()
	assert.Len(t, id, 32)
	// Should be hex.
	for _, c := range id {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'), "expected hex char, got %c", c)
	}
}

func Test_generateID_unique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateID()
		assert.False(t, ids[id], "duplicate ID generated")
		ids[id] = true
	}
}

// ---------------------------------------------------------------------------
// Job.HasFailed
// ---------------------------------------------------------------------------

func TestJobHasFailedMaxTries(t *testing.T) {
	maxTries := 3
	p := &LaravelPayload{MaxTries: &maxTries, Attempts: 3}
	raw := &RawJob{Body: mustMarshal(t, p), Queue: "default"}
	job, err := ParseJob(context.Background(), raw, nil)
	require.NoError(t, err)
	assert.True(t, job.HasFailed())
}

func TestJobHasNotFailedUnderMaxTries(t *testing.T) {
	maxTries := 3
	p := &LaravelPayload{MaxTries: &maxTries, Attempts: 2}
	raw := &RawJob{Body: mustMarshal(t, p), Queue: "default"}
	job, err := ParseJob(context.Background(), raw, nil)
	require.NoError(t, err)
	assert.False(t, job.HasFailed())
}

func TestJobHasFailedRetryUntil(t *testing.T) {
	past := time.Now().Unix() - 100
	p := &LaravelPayload{RetryUntil: &past, Attempts: 1}
	raw := &RawJob{Body: mustMarshal(t, p), Queue: "default"}
	job, err := ParseJob(context.Background(), raw, nil)
	require.NoError(t, err)
	assert.True(t, job.HasFailed())
}

func TestJobHasNotFailedRetryUntilFuture(t *testing.T) {
	future := time.Now().Unix() + 3600
	p := &LaravelPayload{RetryUntil: &future, Attempts: 1}
	raw := &RawJob{Body: mustMarshal(t, p), Queue: "default"}
	job, err := ParseJob(context.Background(), raw, nil)
	require.NoError(t, err)
	assert.False(t, job.HasFailed())
}

func TestJobHasNotFailedNoLimits(t *testing.T) {
	p := &LaravelPayload{Attempts: 100}
	raw := &RawJob{Body: mustMarshal(t, p), Queue: "default"}
	job, err := ParseJob(context.Background(), raw, nil)
	require.NoError(t, err)
	assert.False(t, job.HasFailed())
}

// ---------------------------------------------------------------------------
// Job.BackoffDuration
// ---------------------------------------------------------------------------

func TestJobBackoffDuration(t *testing.T) {
	p := &LaravelPayload{
		Backoff:  []any{float64(5), float64(30), float64(90)},
		Attempts: 2,
	}
	raw := &RawJob{Body: mustMarshal(t, p), Queue: "default"}
	job, err := ParseJob(context.Background(), raw, nil)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, job.BackoffDuration())
}

// ---------------------------------------------------------------------------
// ParseJob from fixture
// ---------------------------------------------------------------------------

func TestParseJobFromFixture(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fixtures/laravel12_payload.json")
	require.NoError(t, err)

	raw := &RawJob{
		Body:  string(data),
		Queue: "default",
	}

	job, err := ParseJob(context.Background(), raw, nil)
	require.NoError(t, err)

	assert.Equal(t, "b4c1e7c6-1234-5678-abcd-1234567890ab", job.UUID())
	assert.Equal(t, `App\Jobs\ProcessReport`, job.DisplayName())
	assert.Equal(t, `App\Jobs\ProcessReport`, job.CommandName())
	assert.Equal(t, 0, job.Attempts())
	assert.Equal(t, "default", job.Queue())
	require.NotNil(t, job.MaxTries())
	assert.Equal(t, 3, *job.MaxTries())
	assert.Equal(t, []string{"important", "client:42"}, job.Tags())

	// PHP deserialized data.
	assert.Equal(t, int64(42), job.GetInt("userId"))
	assert.Equal(t, "rpt_abc123", job.GetString("reportId"))
	assert.Equal(t, "default", job.GetString("queue"))
}

func TestParseJobFromFixtureLaravel13(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fixtures/laravel13_payload.json")
	require.NoError(t, err)

	raw := &RawJob{
		Body:  string(data),
		Queue: "default",
	}

	job, err := ParseJob(context.Background(), raw, nil)
	require.NoError(t, err)

	assert.Equal(t, `App\Jobs\SendNotification`, job.DisplayName())
	assert.Equal(t, 2, job.Attempts())
	require.NotNil(t, job.MaxTries())
	assert.Equal(t, 5, *job.MaxTries())
	require.NotNil(t, job.Timeout())
	assert.Equal(t, 60, *job.Timeout())
	assert.True(t, job.Payload().FailOnTimeout)

	assert.Equal(t, int64(99), job.GetInt("userId"))
}

// ---------------------------------------------------------------------------
// ParseJob accessor edge cases
// ---------------------------------------------------------------------------

func TestJobAccessorsWithNoData(t *testing.T) {
	// A payload with no PHP-serialized command.
	p := &LaravelPayload{
		UUID:        "u",
		DisplayName: "Job",
		Data:        PayloadData{CommandName: "Job", Command: ""},
	}
	raw := &RawJob{Body: mustMarshal(t, p), Queue: "q"}
	job, err := ParseJob(context.Background(), raw, nil)
	require.NoError(t, err)

	assert.Equal(t, "", job.GetString("anything"))
	assert.Equal(t, int64(0), job.GetInt("anything"))
	assert.InDelta(t, 0.0, job.GetFloat("anything"), 1e-9)
	assert.False(t, job.GetBool("anything"))
	assert.Nil(t, job.GetSlice("anything"))
	assert.Nil(t, job.GetMap("anything"))
	assert.Nil(t, job.GetModelID("anything"))
	assert.Nil(t, job.ChainedJobs())
	assert.Equal(t, "", job.BatchID())
}

// ---------------------------------------------------------------------------
// JSON command format
// ---------------------------------------------------------------------------

func TestParseJobWithJSONCommand(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fixtures/json_command_payload.json")
	require.NoError(t, err)

	raw := &RawJob{Body: string(data), Queue: "default"}
	job, err := ParseJob(context.Background(), raw, nil)
	require.NoError(t, err)

	assert.Equal(t, FormatJSON, job.Format())
	assert.Equal(t, `App\Jobs\ProcessUpload`, job.DisplayName())
	assert.Equal(t, `App\Jobs\ProcessUpload`, job.CommandName())

	// Typed accessors work with JSON-decoded data.
	assert.Equal(t, int64(12345), job.GetInt("fileId"))
	assert.Equal(t, "report.pdf", job.GetString("fileName"))
	assert.True(t, job.GetBool("isPublic"))
	assert.InDelta(t, 99.95, job.GetFloat("amount"), 0.001)
	assert.Equal(t, []any{"finance", "q4"}, job.GetSlice("tags"))
	assert.Equal(t, map[string]any{"source": "api", "version": float64(2)}, job.GetMap("metadata"))
}

func TestParseJobWithJSONCommandFormat(t *testing.T) {
	// Build a job with JSON command inline.
	jsonCmd := `{"commandName":"App\\Jobs\\Ping","message":"hello","count":3}`
	p := &LaravelPayload{
		UUID:        "u",
		DisplayName: `App\Jobs\Ping`,
		Job:         LaravelCallQueuedHandler,
		Data:        PayloadData{CommandName: `App\Jobs\Ping`, Command: jsonCmd},
	}
	raw := &RawJob{Body: mustMarshal(t, p), Queue: "q"}
	job, err := ParseJob(context.Background(), raw, nil)
	require.NoError(t, err)

	assert.Equal(t, FormatJSON, job.Format())
	assert.Equal(t, "hello", job.GetString("message"))
	assert.Equal(t, int64(3), job.GetInt("count"))
}

func TestParseJobPHPFormatDetected(t *testing.T) {
	// PHP-serialized command should be detected as FormatPHP.
	phpCmd := "O:22:\"App\\Jobs\\ProcessReport\":1:{s:6:\"userId\";i:42;}"
	p := &LaravelPayload{
		UUID:        "u",
		DisplayName: `App\Jobs\ProcessReport`,
		Job:         LaravelCallQueuedHandler,
		Data:        PayloadData{CommandName: `App\Jobs\ProcessReport`, Command: phpCmd},
	}
	raw := &RawJob{Body: mustMarshal(t, p), Queue: "q"}
	job, err := ParseJob(context.Background(), raw, nil)
	require.NoError(t, err)

	assert.Equal(t, FormatPHP, job.Format())
	assert.Equal(t, int64(42), job.GetInt("userId"))
}

func TestDispatcherJSONFormat(t *testing.T) {
	// Verify that a JSON-format dispatcher produces a parseable JSON command.
	var pushed []byte
	driver := &mockDriver{
		pushFn: func(ctx context.Context, queue string, payload []byte) (string, error) {
			pushed = payload
			return "", nil
		},
	}

	d := NewDispatcher(driver, WithCommandFormat(FormatJSON))
	err := d.Dispatch(context.Background(), `App\Jobs\Test`, map[string]any{
		"key": "value",
		"num": 42,
	})
	require.NoError(t, err)

	// Parse the pushed payload and verify the command is valid JSON.
	var envelope LaravelPayload
	require.NoError(t, json.Unmarshal(pushed, &envelope))
	assert.Equal(t, `App\Jobs\Test`, envelope.Data.CommandName)

	// The command should be valid JSON, not PHP serialize.
	var cmdMap map[string]any
	require.NoError(t, json.Unmarshal([]byte(envelope.Data.Command), &cmdMap))
	assert.Equal(t, "value", cmdMap["key"])
	assert.Equal(t, float64(42), cmdMap["num"])
	assert.Equal(t, `App\Jobs\Test`, cmdMap["commandName"])
}

// mockDriver for testing dispatch without Redis.
type mockDriver struct {
	pushFn func(ctx context.Context, queue string, payload []byte) (string, error)
}

func (m *mockDriver) Push(ctx context.Context, queue string, payload []byte) (string, error) {
	return m.pushFn(ctx, queue, payload)
}
func (m *mockDriver) Later(context.Context, string, time.Duration, []byte) (string, error) {
	return "", nil
}
func (m *mockDriver) Pop(context.Context, string) (*RawJob, error)          { return nil, nil }
func (m *mockDriver) Delete(context.Context, *RawJob) error                 { return nil }
func (m *mockDriver) Release(context.Context, *RawJob, time.Duration) error { return nil }
func (m *mockDriver) Size(context.Context, string) (int64, error)           { return 0, nil }
func (m *mockDriver) Clear(context.Context, string) (int64, error)          { return 0, nil }
func (m *mockDriver) MigrateExpiredJobs(context.Context, string) error      { return nil }
func (m *mockDriver) Close() error                                          { return nil }

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}
