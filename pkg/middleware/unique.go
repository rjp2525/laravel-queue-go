package middleware

import (
	"context"
	"crypto/rand"
	"time"

	"github.com/redis/go-redis/v9"
)

// luaReleaseLock deletes the lock only if it still holds the caller's token.
// KEYS[1] = lock key, ARGV[1] = owner token
var luaReleaseLock = redis.NewScript(`
if redis.call('get', KEYS[1]) == ARGV[1] then
    return redis.call('del', KEYS[1])
end
return 0
`)

const (
	uniqueKeyPrefix      = "laravel_unique_job:"
	overlappingKeyPrefix = "laravel_overlapping:"
)

func Unique(client redis.Cmdable, keyFunc KeyFunc, ttl time.Duration) Middleware {
	return lockMiddleware(client, keyFunc, ttl, uniqueKeyPrefix, func() error {
		return nil // skip silently when lock not acquired
	})
}

func WithoutOverlapping(client redis.Cmdable, keyFunc KeyFunc, ttl time.Duration) Middleware {
	return lockMiddleware(client, keyFunc, ttl, overlappingKeyPrefix, func() error {
		return &ReleaseError{Delay: 10 * time.Second}
	})
}

func lockMiddleware(client redis.Cmdable, keyFunc KeyFunc, ttl time.Duration, prefix string, onConflict func() error) Middleware {
	return func(ctx context.Context, job Job, next func() error) error {
		key := prefix + keyFunc(job)

		// A random owner token makes the release safe: if the job outlives
		// ttl, the lock expires and another worker may take it. Deleting the
		// key unconditionally would then remove that worker's lock.
		owner := rand.Text()

		acquired, err := client.SetNX(ctx, key, owner, ttl).Result()
		if err != nil {
			return err
		}
		if !acquired {
			return onConflict()
		}

		// Release even if ctx was cancelled while the job ran; a lock left
		// behind would block the job until ttl, or forever when ttl is 0.
		defer func() {
			_ = luaReleaseLock.Run(context.WithoutCancel(ctx), client, []string{key}, owner).Err()
		}()

		return next()
	}
}

type ReleaseError struct {
	Delay time.Duration
}

func (e *ReleaseError) Error() string {
	return "job released back to queue"
}
