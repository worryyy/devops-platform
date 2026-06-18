package cache

import "context"

type Redis interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttlSeconds int) error
	Delete(ctx context.Context, key string) error
}

type NoopRedis struct{}

func (NoopRedis) Get(ctx context.Context, key string) (string, error) {
	return "", nil
}

func (NoopRedis) Set(ctx context.Context, key, value string, ttlSeconds int) error {
	return nil
}

func (NoopRedis) Delete(ctx context.Context, key string) error {
	return nil
}
