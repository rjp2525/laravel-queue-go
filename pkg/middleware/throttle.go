package middleware

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const throttleKeyPrefix = "laravel_throttle_exceptions:"

// luaThrottle atomically increments and sets TTL if needed (same pattern as luaRateLimit).
var luaThrottle = redis.NewScript(`
local count = redis.call('incr', KEYS[1])
if count == 1 then
    redis.call('expire', KEYS[1], ARGV[1])
end
return count
`)

// ThrottlesExceptions releases jobs after too many consecutive exceptions.
// After maxExceptions within decayMinutes, the job is released with the decay duration as delay.
func ThrottlesExceptions(client redis.Cmdable, keyFunc KeyFunc, maxExceptions int, decayMinutes int) Middleware {
	return func(ctx context.Context, job Job, next func() error) error {
		err := next()
		if err == nil {
			return nil
		}

		key := throttleKeyPrefix + keyFunc(job)
		decay := time.Duration(decayMinutes) * time.Minute

		count, redisErr := luaThrottle.Run(ctx, client,
			[]string{key},
			int(decay.Seconds()),
		).Int64()
		if redisErr != nil {
			return err
		}

		if count >= int64(maxExceptions) {
			_ = client.Del(ctx, key).Err()
			return &ReleaseError{Delay: decay}
		}

		return err
	}
}
