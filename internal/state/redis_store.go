package state

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewRedisStore(rdb *redis.Client, ttl time.Duration) *RedisStore {
	return &RedisStore{rdb: rdb, ttl: ttl}
}

func (r *RedisStore) Save(ctx context.Context, st *ExecutionState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("execstate:%s", st.ID)
	return r.rdb.Set(ctx, key, b, r.ttl).Err()
}

func (r *RedisStore) Load(ctx context.Context, id string) (*ExecutionState, error) {
	key := fmt.Sprintf("execstate:%s", id)
	data, err := r.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}
	var st ExecutionState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (r *RedisStore) Delete(ctx context.Context, id string) error {
	key := fmt.Sprintf("execstate:%s", id)
	return r.rdb.Del(ctx, key).Err()
}
