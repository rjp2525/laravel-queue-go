package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rjp2525/laravel-queue-go/v2/pkg/events"
	"github.com/rjp2525/laravel-queue-go/v2/pkg/failed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackoffSecondsLaravelString(t *testing.T) {
	// Laravel writes backoff as a comma-joined string.
	p := &LaravelPayload{Backoff: "1,5,10"}
	assert.Equal(t, 1, p.BackoffSeconds(1))
	assert.Equal(t, 5, p.BackoffSeconds(2))
	assert.Equal(t, 10, p.BackoffSeconds(3))
	assert.Equal(t, 10, p.BackoffSeconds(9), "past the end uses the last entry")

	assert.Equal(t, 30, (&LaravelPayload{Backoff: "30"}).BackoffSeconds(4))
	assert.Equal(t, 0, (&LaravelPayload{Backoff: ""}).BackoffSeconds(1))
	assert.Equal(t, 0, (&LaravelPayload{Backoff: "abc"}).BackoffSeconds(1), "like PHP's (int) cast")
}

func TestBackoffIsParsedFromLaravelPayload(t *testing.T) {
	var p LaravelPayload
	require.NoError(t, json.Unmarshal([]byte(`{"backoff":"10,60","attempts":2}`), &p))
	assert.True(t, p.HasBackoff())
	assert.Equal(t, 60, p.BackoffSeconds(p.Attempts))
}

func TestBackoffIsMarshalledAsLaravelString(t *testing.T) {
	for name, tc := range map[string]struct {
		in   any
		want string
	}{
		"int":        {10, `"10"`},
		"int slice":  {[]int{1, 5, 10}, `"1,5,10"`},
		"any slice":  {[]any{float64(1), float64(5)}, `"1,5"`},
		"duration":   {90 * time.Second, `"90"`},
		"durations":  {[]time.Duration{time.Second, time.Minute}, `"1,60"`},
		"string":     {"3,6", `"3,6"`},
		"nil":        {nil, `null`},
		"empty list": {[]int{}, `null`},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(NewPayload("Job", "cmd", WithBackoff(tc.in)))
			require.NoError(t, err)

			var got struct {
				Backoff json.RawMessage `json:"backoff"`
			}
			require.NoError(t, json.Unmarshal(data, &got))
			assert.JSONEq(t, tc.want, string(got.Backoff))
		})
	}
}

func jobWith(t *testing.T, p *LaravelPayload) *Job {
	t.Helper()
	job, err := ParseJob(context.Background(), &RawJob{Body: mustMarshal(t, p), Queue: "default"}, nil)
	require.NoError(t, err)
	return job
}

func TestMaxTriesZeroMeansUnlimited(t *testing.T) {
	zero := 0
	job := jobWith(t, &LaravelPayload{MaxTries: &zero, Attempts: 50})
	assert.False(t, job.HasFailed())
	assert.False(t, job.alreadyExceedsMaxAttempts(1), "an explicit 0 overrides the worker default")
}

func TestRetryUntilTakesPrecedenceOverMaxTries(t *testing.T) {
	one := 1
	future := time.Now().Add(time.Hour).Unix()
	past := time.Now().Add(-time.Hour).Unix()

	job := jobWith(t, &LaravelPayload{MaxTries: &one, RetryUntil: &future, Attempts: 5})
	assert.False(t, job.HasFailed(), "before the deadline maxTries is ignored")

	job = jobWith(t, &LaravelPayload{RetryUntil: &past, Attempts: 1})
	assert.True(t, job.HasFailed())
	assert.True(t, job.alreadyExceedsMaxAttempts(0))
}

func TestWorkerDefaultMaxTriesAppliesWhenPayloadHasNone(t *testing.T) {
	job := jobWith(t, &LaravelPayload{Attempts: 1})
	assert.False(t, job.willExceedMaxAttempts(0), "0 keeps retrying")
	assert.True(t, job.willExceedMaxAttempts(1), "like queue:work --tries=1")

	three := 3
	job = jobWith(t, &LaravelPayload{MaxTries: &three, Attempts: 1})
	assert.False(t, job.willExceedMaxAttempts(1), "the payload wins over the worker default")
}

// scriptedDriver hands out a fixed list of jobs and records what happens to them.
type scriptedDriver struct {
	mockDriver
	mu       sync.Mutex
	jobs     []*RawJob
	released []time.Duration
	deleted  int
}

func (d *scriptedDriver) Pop(context.Context, string) (*RawJob, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.jobs) == 0 {
		return nil, nil
	}
	job := d.jobs[0]
	d.jobs = d.jobs[1:]
	return job, nil
}

func (d *scriptedDriver) Release(_ context.Context, _ *RawJob, delay time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.released = append(d.released, delay)
	return nil
}

func (d *scriptedDriver) Delete(context.Context, *RawJob) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleted++
	return nil
}

type recordingFailedLogger struct{ records []failed.Record }

func (r *recordingFailedLogger) Log(_ context.Context, rec failed.Record) error {
	r.records = append(r.records, rec)
	return nil
}

func reservedJob(attempts int, extra string) *RawJob {
	body := func(n int) string {
		return fmt.Sprintf(`{"uuid":"u-1","displayName":"App\\Jobs\\Flaky","job":"j","data":{"commandName":"App\\Jobs\\Flaky","command":""},"attempts":%d%s}`, n, extra)
	}
	return &RawJob{Body: body(attempts - 1), Reserved: body(attempts), Queue: "default"}
}

func TestWorkerFailsJobThatAlreadyExceededMaxAttemptsWithoutRunningIt(t *testing.T) {
	// maxTries 2, but this is the third reservation: both earlier runs died
	// without reporting back.
	driver := &scriptedDriver{jobs: []*RawJob{reservedJob(3, `,"maxTries":2`)}}
	failedLog := &recordingFailedLogger{}

	worker := NewWorker(driver, WorkerOptions{StopOnEmpty: true})
	worker.SetFailedProvider(failedLog)
	ran := false
	worker.Register(`App\Jobs\Flaky`, func(context.Context, *Job) error { ran = true; return nil })
	var failure error
	worker.On(events.JobFailed, func(e events.Event) { failure = e.Error })

	require.NoError(t, worker.Run(context.Background()))

	assert.False(t, ran, "the handler must not run again")
	assert.Equal(t, 1, driver.deleted)
	assert.Empty(t, driver.released)
	require.Len(t, failedLog.records, 1)
	assert.True(t, errors.Is(failure, ErrMaxAttemptsExceeded))
}

func TestWorkerRetryDelay(t *testing.T) {
	boom := errors.New("boom")

	for name, tc := range map[string]struct {
		extra string
		opts  WorkerOptions
		want  []time.Duration
	}{
		"payload schedule":            {`,"backoff":"7,20"`, WorkerOptions{}, []time.Duration{20 * time.Second}},
		"worker default":              {``, WorkerOptions{Backoff: 4 * time.Second}, []time.Duration{4 * time.Second}},
		"payload beats worker":        {`,"backoff":"0"`, WorkerOptions{Backoff: 4 * time.Second}, []time.Duration{0}},
		"no backoff anywhere":         {``, WorkerOptions{}, []time.Duration{0}},
		"worker tries exhausted":      {``, WorkerOptions{MaxTries: 2}, nil},
		"payload tries not exhausted": {`,"maxTries":5`, WorkerOptions{MaxTries: 1}, []time.Duration{0}},
	} {
		t.Run(name, func(t *testing.T) {
			driver := &scriptedDriver{jobs: []*RawJob{reservedJob(2, tc.extra)}}
			tc.opts.StopOnEmpty = true
			worker := NewWorker(driver, tc.opts)
			worker.Register(`App\Jobs\Flaky`, func(context.Context, *Job) error { return boom })

			require.NoError(t, worker.Run(context.Background()))
			assert.Equal(t, tc.want, driver.released)
			if tc.want == nil {
				assert.Equal(t, 1, driver.deleted, "exhausted jobs are deleted and failed")
			}
		})
	}
}
