package middleware

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const rateLimitKeyPrefix = "laravel_queue_rate_limit:"

// luaRateLimit atomically increments and sets TTL if needed.
var luaRateLimit = redis.NewScript(`
local count = redis.call('incr', KEYS[1])
if count == 1 then
    redis.call('expire', KEYS[1], ARGV[1])
end
return count
`)

func RateLimited(client redis.Cmdable, key string, maxAttempts int, decay time.Duration) Middleware {
	return func(ctx context.Context, job Job, next func() error) error {
		limiterKey := rateLimitKeyPrefix + key

		count, err := luaRateLimit.Run(ctx, client,
			[]string{limiterKey},
			int(decay.Seconds()),
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
			var releaseErr *ReleaseError
			if !errors.As(processErr, &releaseErr) {
				_ = client.Decr(ctx, limiterKey).Err()
			}
		}

		return processErr
	}
}
