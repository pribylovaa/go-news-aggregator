package cache

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RefreshEntry описывает данные, которые мы храним в Redis по хэшу refresh-токена.
type RefreshEntry struct {
	UserID    uuid.UUID
	Revoked   bool
	ExpiresAt time.Time
}

// RefreshCache — минимальный контракт кэша refresh-токенов.
type RefreshCache interface {
	// Get возвращает запись и признак её наличия в кэше.
	Get(ctx context.Context, hash string) (*RefreshEntry, bool, error)
	// Set сохраняет запись с TTL (обычно ExpiresAt-now).
	Set(ctx context.Context, hash string, e *RefreshEntry, ttl time.Duration) error
	// MarkRevoked помечает ключ revoked=true, сохраняя остаточный TTL.
	MarkRevoked(ctx context.Context, hash string) error
	// Close закрывает клиент Redis.
	Close() error
}

type redisCache struct {
	rdb    *redis.Client
	prefix string
}

// NewRedisCache создаёт клиент Redis из URL (например, redis://:pass@host:6379/0).
// Если prefix пустой — используется "auth:rt:".
func NewRedisCache(redisURL, prefix string) (RefreshCache, error) {
	if prefix == "" {
		prefix = "auth:rt:"
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	rdb := redis.NewClient(opt)

	// Fail-fast на старте.
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}

	return &redisCache{rdb: rdb, prefix: prefix}, nil
}

func (c *redisCache) key(hash string) string { return c.prefix + hash }

// Храним как Redis Hash с полями: uid, rev (0/1), exp (unix).
func (c *redisCache) Get(ctx context.Context, hash string) (*RefreshEntry, bool, error) {
	m, err := c.rdb.HGetAll(ctx, c.key(hash)).Result()
	if err != nil {
		return nil, false, err
	}

	if len(m) == 0 {
		return nil, false, nil
	}

	uid, err := uuid.Parse(m["uid"])
	if err != nil {
		return nil, false, err
	}
	rev := m["rev"] == "1"

	expUnix, err := strconv.ParseInt(m["exp"], 10, 64)
	if err != nil {
		return nil, false, err
	}

	return &RefreshEntry{
		UserID:    uid,
		Revoked:   rev,
		ExpiresAt: time.Unix(expUnix, 0).UTC(),
	}, true, nil
}

func (c *redisCache) Set(ctx context.Context, hash string, e *RefreshEntry, ttl time.Duration) error {
	kv := map[string]string{
		"uid": e.UserID.String(),
		"rev": boolTo01(e.Revoked),
		"exp": strconv.FormatInt(e.ExpiresAt.Unix(), 10),
	}

	pipe := c.rdb.TxPipeline()
	pipe.HSet(ctx, c.key(hash), kv)
	pipe.Expire(ctx, c.key(hash), ttl)

	_, err := pipe.Exec(ctx)
	return err
}

func (c *redisCache) MarkRevoked(ctx context.Context, hash string) error {
	return c.rdb.HSet(ctx, c.key(hash), "rev", "1").Err()
}

func (c *redisCache) Close() error { return c.rdb.Close() }

func boolTo01(b bool) string {
	if b {
		return "1"
	}

	return "0"
}
