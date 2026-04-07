package middleware

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

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

		acquired, err := client.SetNX(ctx, key, 1, ttl).Result()
		if err != nil {
			return err
		}
		if !acquired {
			return onConflict()
		}

		err = next()
		_ = client.Del(ctx, key).Err()
		return err
	}
}

type ReleaseError struct {
	Delay time.Duration
}

func (e *ReleaseError) Error() string {
	return "job released back to queue"
}
