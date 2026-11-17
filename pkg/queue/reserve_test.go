package queue

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// releaseRecorder captures what a Job hands to the driver on Release/Delete.
type releaseRecorder struct {
	mockDriver
	released []string
	deleted  []string
}

func (r *releaseRecorder) Release(_ context.Context, job *RawJob, _ time.Duration) error {
	r.released = append(r.released, job.reserved())
	return nil
}

func (r *releaseRecorder) Delete(_ context.Context, job *RawJob) error {
	r.deleted = append(r.deleted, job.reserved())
	return nil
}

func TestParseJobReadsReservedPayload(t *testing.T) {
	raw := &RawJob{
		Body:     `{"uuid":"u","displayName":"X","job":"j","data":{"commandName":"X","command":""},"attempts":0}`,
		Reserved: `{"uuid":"u","displayName":"X","job":"j","data":{"commandName":"X","command":""},"attempts":1}`,
		Queue:    "default",
	}
	job, err := ParseJob(context.Background(), raw, &mockDriver{})
	require.NoError(t, err)
	assert.Equal(t, 1, job.Attempts())
}

func TestJobReleaseAndDeleteUseReservedPayloadVerbatim(t *testing.T) {
	const reserved = `{"attempts":1,"uuid":"u","unknownKey":{"kept":true},"displayName":"X","job":"j","data":{"commandName":"X","command":""}}`
	raw := &RawJob{Body: `{"attempts":0}`, Reserved: reserved, Queue: "default"}
	rec := &releaseRecorder{}

	job, err := ParseJob(context.Background(), raw, rec)
	require.NoError(t, err)

	require.NoError(t, job.Release(time.Second))
	require.NoError(t, job.Delete())

	assert.Equal(t, []string{reserved}, rec.released)
	assert.Equal(t, []string{reserved}, rec.deleted)
	assert.Equal(t, `{"attempts":0}`, raw.Body, "Release must not rewrite the raw body")
	assert.Equal(t, 1, job.Attempts(), "Release must not bump attempts a second time")
}

func TestRawJobReservedFallsBackToBody(t *testing.T) {
	raw := &RawJob{Body: `{"attempts":0}`}
	assert.Equal(t, raw.Body, raw.reserved())
}

func TestPayloadTagsToleratesCjsonEmptyObject(t *testing.T) {
	for name, tc := range map[string]struct {
		json string
		want []string
	}{
		"array":        {`{"tags":["a","b"]}`, []string{"a", "b"}},
		"empty array":  {`{"tags":[]}`, []string{}},
		"empty object": {`{"tags":{}}`, nil},
		"null":         {`{"tags":null}`, nil},
		"absent":       {`{}`, nil},
	} {
		t.Run(name, func(t *testing.T) {
			var p LaravelPayload
			require.NoError(t, p.UnmarshalJSON([]byte(tc.json)))
			assert.Equal(t, tc.want, p.Tags)
		})
	}
}
