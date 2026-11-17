package queue

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Payloads as each Laravel major writes them for an object job on the Redis
// queue, built from Queue::createObjectPayload / RedisQueue::createPayloadArray
// at the 10.x, 11.x, 12.x and 13.x branch tips.
const (
	payloadLaravel10 = `{"uuid":"a","displayName":"App\\Jobs\\J","job":"Illuminate\\Queue\\CallQueuedHandler@call","maxTries":null,"maxExceptions":null,"failOnTimeout":false,"backoff":null,"timeout":null,"retryUntil":null,"data":{"commandName":"App\\Jobs\\J","command":"O:10:\"App\\Jobs\\J\":0:{}"},"id":"x","attempts":0}`
	payloadLaravel11 = `{"uuid":"a","displayName":"App\\Jobs\\J","job":"Illuminate\\Queue\\CallQueuedHandler@call","maxTries":null,"maxExceptions":null,"failOnTimeout":false,"backoff":null,"timeout":null,"retryUntil":null,"data":{"commandName":"App\\Jobs\\J","command":"O:10:\"App\\Jobs\\J\":0:{}"},"illuminate:log:context":{"data":{"trace":"s:3:\"abc\";"},"hidden":[]},"id":"x","attempts":0}`
	payloadLaravel12 = `{"uuid":"a","displayName":"App\\Jobs\\J","job":"Illuminate\\Queue\\CallQueuedHandler@call","maxTries":null,"maxExceptions":null,"failOnTimeout":false,"backoff":null,"timeout":null,"retryUntil":null,"data":{"commandName":"App\\Jobs\\J","command":"O:10:\"App\\Jobs\\J\":0:{}","batchId":null},"createdAt":1760000000,"id":"x","attempts":0,"delay":null}`
	payloadLaravel13 = `{"uuid":"a","displayName":"App\\Jobs\\J","job":"Illuminate\\Queue\\CallQueuedHandler@call","maxTries":null,"maxExceptions":null,"failOnTimeout":false,"countCrashesAsExceptions":false,"backoff":null,"timeout":null,"retryUntil":null,"deleteWhenMissingModels":false,"data":{"commandName":"App\\Jobs\\J","command":"O:10:\"App\\Jobs\\J\":0:{}","batchId":null},"createdAt":1760000000,"id":"x","attempts":0,"delay":null}`
)

func TestParseLaravelVersion(t *testing.T) {
	for in, want := range map[string]LaravelVersion{
		"": LaravelAuto, "auto": LaravelAuto, "AUTO": LaravelAuto,
		"10": Laravel10, "11.x": Laravel11, "v12.3.1": Laravel12, "^13.0": Laravel13, " 13 ": Laravel13,
	} {
		got, err := ParseLaravelVersion(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"9", "14", "twelve", "0"} {
		_, err := ParseLaravelVersion(in)
		assert.Error(t, err, in)
	}
}

func TestLaravelVersionRedisPrefix(t *testing.T) {
	assert.Equal(t, "laravel_database_", Laravel10.RedisPrefix(""))
	assert.Equal(t, "laravel_database_", Laravel11.RedisPrefix("Laravel"))
	assert.Equal(t, "laravel-database-", Laravel12.RedisPrefix("Laravel"))
	assert.Equal(t, "laravel-database-", Laravel13.RedisPrefix(""))
	assert.Equal(t, DefaultRedisPrefix, LaravelAuto.RedisPrefix(""), "auto keeps the historical default")

	assert.Equal(t, "my_shop_2_database_", Laravel11.RedisPrefix("My Shop 2"))
	assert.Equal(t, "my-shop-2-database-", Laravel13.RedisPrefix("  My  Shop_2! "))
}

func TestNewRedisDriverPrefixFollowsVersion(t *testing.T) {
	d := NewRedisDriver(nil, RedisDriverConfig{LaravelVersion: Laravel10})
	assert.Equal(t, "laravel_database_queues:default", d.keys.Ready("default"))

	d = NewRedisDriver(nil, RedisDriverConfig{LaravelVersion: Laravel10, Prefix: "custom:"})
	assert.Equal(t, "custom:queues:default", d.keys.Ready("default"), "an explicit prefix always wins")
}

func TestDetectLaravelVersion(t *testing.T) {
	for name, tc := range map[string]struct {
		payload string
		want    LaravelVersion
	}{
		"10":                     {payloadLaravel10, Laravel10},
		"11 with context":        {payloadLaravel11, Laravel11},
		"12":                     {payloadLaravel12, Laravel12},
		"13":                     {payloadLaravel13, Laravel13},
		"12 after cjson reserve": {`{"delay":null,"attempts":1,"job":"j","data":{}}`, Laravel12},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := DetectLaravelVersion([]byte(tc.payload))
			require.True(t, ok)
			assert.Equal(t, tc.want, got)
		})
	}

	for _, notAJob := range []string{``, `[]`, `"x"`, `{"foo":1}`, `not json`} {
		_, ok := DetectLaravelVersion([]byte(notAJob))
		assert.False(t, ok, notAJob)
	}
}

func TestPayloadKeysMatchLaravelVersion(t *testing.T) {
	keyOrder := func(t *testing.T, data []byte) []string {
		t.Helper()
		var keys []string
		for _, m := range regexp.MustCompile(`[{,]"([A-Za-z:]+)":`).FindAllSubmatch(data, -1) {
			keys = append(keys, string(m[1]))
		}
		return keys
	}
	// tags and pushedAt are Horizon's keys; this package has always sent them.
	strip := func(keys []string) []string {
		var out []string
		for _, k := range keys {
			if k != "tags" && k != "pushedAt" {
				out = append(out, k)
			}
		}
		return out
	}

	for v, fixture := range map[LaravelVersion]string{
		Laravel10: payloadLaravel10,
		Laravel11: payloadLaravel10, // 11 only differs by the optional context key
		Laravel12: payloadLaravel12,
		Laravel13: payloadLaravel13,
	} {
		t.Run(v.String(), func(t *testing.T) {
			data, err := json.Marshal(NewPayload(`App\Jobs\J`, `O:10:"App\Jobs\J":0:{}`, WithLaravelVersion(v)))
			require.NoError(t, err)

			assert.Equal(t, keyOrder(t, []byte(fixture)), strip(keyOrder(t, data)),
				"same keys in the same order as a native Laravel %s payload", v)

			detected, ok := DetectLaravelVersion(data)
			require.True(t, ok)
			if v == Laravel11 {
				v = Laravel10 // indistinguishable without a context
			}
			assert.Equal(t, v, detected, "our own payloads must detect as the version they target")
		})
	}
}

func TestPayloadAutoWritesLatestFormat(t *testing.T) {
	data, err := json.Marshal(NewPayload("J", "c"))
	require.NoError(t, err)
	v, ok := DetectLaravelVersion(data)
	require.True(t, ok)
	assert.Equal(t, LatestLaravel, v)
}

func TestPayloadRoundTripsVersionKeys(t *testing.T) {
	var p LaravelPayload
	require.NoError(t, json.Unmarshal([]byte(`{"job":"j","countCrashesAsExceptions":true,"deleteWhenMissingModels":true,"createdAt":1760000000,"delay":30,"data":{"commandName":"J","command":"c","batchId":"b-1"},"illuminate:log:context":{"data":{},"hidden":{}}}`), &p))

	assert.True(t, p.CountCrashesAsExceptions)
	assert.True(t, p.DeleteWhenMissingModels)
	require.NotNil(t, p.CreatedAt)
	assert.Equal(t, int64(1760000000), *p.CreatedAt)
	require.NotNil(t, p.Delay)
	assert.Equal(t, 30, *p.Delay)
	require.NotNil(t, p.Data.BatchID)
	assert.Equal(t, "b-1", *p.Data.BatchID)
	assert.JSONEq(t, `{"data":{},"hidden":{}}`, string(p.LogContext))
}

func TestBackoffFallsBackToDispatchDelay(t *testing.T) {
	// Job::backoff() is payload['backoff'] ?? payload['delay'].
	p := NewPayload("J", "c", WithDelay(45*time.Second))
	assert.True(t, p.HasBackoff())
	assert.Equal(t, 45, p.BackoffSeconds(1))

	p = NewPayload("J", "c", WithDelay(45*time.Second), WithBackoff("5"))
	assert.Equal(t, 5, p.BackoffSeconds(1), "an explicit backoff wins")
}

func TestKeysHashTag(t *testing.T) {
	k := &Keys{Prefix: "p:", HashTag: true}
	assert.Equal(t, "p:queues:{default}", k.Ready("default"))
	assert.Equal(t, "p:queues:{default}:delayed", k.Delayed("default"))
	assert.Equal(t, "p:queues:{default}:reserved", k.Reserved("default"))
	assert.Equal(t, "p:queues:{default}:notify", k.Notify("default"))
	assert.Equal(t, "p:queues:{tenant}:mail", k.Ready("{tenant}:mail"), "a name with its own tag is left alone")
	assert.Equal(t, "p:queues:{a{}}", k.Ready("a{}"), "an empty tag is not a hash tag")

	assert.Equal(t, "p:queues:default", NewKeys("p:").Ready("default"))
}

func TestParseQueueKey(t *testing.T) {
	for key, want := range map[string]QueueLocation{
		"laravel_database_queues:default":          {"laravel_database_", "default"},
		"laravel-database-queues:emails:delayed":   {"laravel-database-", "emails"},
		"queues:high:reserved":                     {"", "high"},
		"app-database-queues:{default}:notify":     {"app-database-", "default"},
		"app-database-queues:{tenant}:mail:notify": {"app-database-", "{tenant}:mail"},
	} {
		got, ok := parseQueueKey(key)
		require.True(t, ok, key)
		assert.Equal(t, want, got, key)
	}
	_, ok := parseQueueKey("laravel_cache_:something")
	assert.False(t, ok)
}

// detectingDriver is a Driver that reports a Laravel version and captures pushes.
type detectingDriver struct {
	mockDriver
	version LaravelVersion
	probes  int
	pushed  [][]byte
}

func (d *detectingDriver) DetectLaravelVersion(context.Context, string) (LaravelVersion, error) {
	d.probes++
	return d.version, nil
}

func (d *detectingDriver) Push(_ context.Context, _ string, payload []byte) (string, error) {
	d.pushed = append(d.pushed, payload)
	return "", nil
}

func (d *detectingDriver) Later(_ context.Context, _ string, _ time.Duration, payload []byte) (string, error) {
	d.pushed = append(d.pushed, payload)
	return "", nil
}

func TestDispatcherAutoDetectsVersionOnce(t *testing.T) {
	driver := &detectingDriver{version: Laravel10}
	d := NewDispatcher(driver)

	for range 3 {
		require.NoError(t, d.Dispatch(context.Background(), "J", nil))
	}
	assert.Equal(t, 1, driver.probes, "the detected version is cached")

	v, ok := DetectLaravelVersion(driver.pushed[2])
	require.True(t, ok)
	assert.Equal(t, Laravel10, v)
}

func TestDispatcherAutoFallsBackToLatestAndRetriesLater(t *testing.T) {
	driver := &detectingDriver{version: LaravelAuto} // empty queue: nothing to learn from
	d := NewDispatcher(driver)

	require.NoError(t, d.Dispatch(context.Background(), "J", nil))
	require.NoError(t, d.Dispatch(context.Background(), "J", nil))
	assert.Equal(t, 1, driver.probes, "an unanswered probe is not repeated on every dispatch")

	v, _ := DetectLaravelVersion(driver.pushed[0])
	assert.Equal(t, LatestLaravel, v)

	// Once the probe interval has passed and Laravel has pushed a job, the
	// dispatcher adopts its version.
	d.lastProbed = time.Now().Add(-2 * detectInterval)
	driver.version = Laravel12
	require.NoError(t, d.Dispatch(context.Background(), "J", nil))
	v, _ = DetectLaravelVersion(driver.pushed[2])
	assert.Equal(t, Laravel12, v)
}

func TestDispatcherExplicitVersionSkipsDetection(t *testing.T) {
	driver := &detectingDriver{version: Laravel13}
	d := NewDispatcher(driver, ForLaravel(Laravel11))

	require.NoError(t, d.Later(context.Background(), time.Minute, "J", nil))
	assert.Zero(t, driver.probes)

	var keys map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(driver.pushed[0], &keys))
	assert.NotContains(t, keys, "delay", "Laravel 11 payloads have no delay key")
	assert.NotContains(t, keys, "createdAt")
}

func TestDispatcherLaterRecordsDelayForLaravel12(t *testing.T) {
	driver := &detectingDriver{}
	d := NewDispatcher(driver, ForLaravel(Laravel12))

	require.NoError(t, d.Later(context.Background(), 90*time.Second, "J", nil))
	require.NoError(t, d.DispatchWithOptions(context.Background(), DispatchOptions{Job: "J", Delay: 30 * time.Second}))

	for i, want := range []string{"90", "30"} {
		var keys map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(driver.pushed[i], &keys))
		assert.Equal(t, want, string(keys["delay"]))
	}
}

func TestJobLaravelVersion(t *testing.T) {
	job, err := ParseJob(context.Background(), &RawJob{Body: payloadLaravel13, Queue: "default"}, nil)
	require.NoError(t, err)
	assert.Equal(t, Laravel13, job.LaravelVersion())
}
