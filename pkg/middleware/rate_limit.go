package middleware

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const rateLimitKeyPrefix = "laravel_queue_rate_limit:"

// luaRateLimit atomically increments and sets the TTL (in milliseconds) when
// the window starts, or when the key has lost its TTL.
var luaRateLimit = redis.NewScript(`
local count = redis.call('incr', KEYS[1])
if count == 1 or redis.call('pttl', KEYS[1]) < 0 then
    redis.call('pexpire', KEYS[1], ARGV[1])
end
return count
`)

// windowMillis converts a limiter window to the PEXPIRE argument. Windows are
// at least 1ms: a TTL of zero would delete the counter on the spot.
func windowMillis(d time.Duration) int64 {
	return max(d.Milliseconds(), 1)
}

// RateLimited allows maxAttempts jobs per decay window for the given key and
// releases the rest back to the queue for decay.
func RateLimited(client redis.Cmdable, key string, maxAttempts int, decay time.Duration) Middleware {
	return func(ctx context.Context, job Job, next func() error) error {
		limiterKey := rateLimitKeyPrefix + key

		count, err := luaRateLimit.Run(ctx, client,
			[]string{limiterKey},
			windowMillis(decay),
		).Int64()
		if err != nil {
			return fmt.Errorf("rate limit: %w", err)
		}

		if count > int64(maxAttempts) {
			_ = client.Decr(ctx, limiterKey).Err()
			return &ReleaseError{Delay: decay}
		}

		processErr := next()

		if processErr != nil {
			if _, released := errors.AsType[*ReleaseError](processErr); !released {
				_ = client.Decr(ctx, limiterKey).Err()
			}
		}

		return processErr
	}
}
