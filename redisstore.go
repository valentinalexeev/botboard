package main

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"tailscale.com/ipn"
)

// redisStore keeps tsnet's node state in one Redis hash (field = state id), so many small
// machines can share a single Redis instead of each needing a persistent volume.
type redisStore struct {
	rdb *redis.Client
	key string
}

func newRedisStore(url, key string) (*redisStore, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	rs := &redisStore{rdb: redis.NewClient(opt), key: key}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return rs, rs.rdb.Ping(ctx).Err()
}

func (r *redisStore) ReadState(id ipn.StateKey) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, err := r.rdb.HGet(ctx, r.key, string(id)).Bytes()
	if err == redis.Nil {
		return nil, ipn.ErrStateNotExist
	}
	return b, err
}

func (r *redisStore) WriteState(id ipn.StateKey, bs []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if bs == nil {
		return r.rdb.HDel(ctx, r.key, string(id)).Err()
	}
	return r.rdb.HSet(ctx, r.key, string(id), bs).Err()
}
