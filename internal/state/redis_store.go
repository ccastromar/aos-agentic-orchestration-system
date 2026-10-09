package state

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	rdb *redis.Client
}

func NewRedisStore(rdb *redis.Client, _ time.Duration) *RedisStore {
	return &RedisStore{rdb: rdb}
}

func (r *RedisStore) Get(ctx context.Context, key string) (string, error) {
	val, err := r.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

func (r *RedisStore) Set(ctx context.Context, key string, val string, ttl time.Duration) error {
	return r.rdb.Set(ctx, key, val, ttl).Err()
}

// SetPersistent writes execution state without an expiration. Session state
// continues to use Set with the configured TTL.
func (r *RedisStore) SetPersistent(ctx context.Context, key, val string) error {
	return r.rdb.Set(ctx, key, val, 0).Err()
}

func (r *RedisStore) Delete(ctx context.Context, key string) error {
	return r.rdb.Del(ctx, key).Err()
}

func (r *RedisStore) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	// For production, SCAN is preferred over KEYS
	var keys []string
	iter := r.rdb.Scan(ctx, 0, prefix+"*", 0).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}
