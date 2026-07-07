package repository

import (
	"context"
	// "encoding/json"
	"errors"
	"time"

	"github.com/goccy/go-json"
	"github.com/redis/go-redis/v9"

	"github.com/TranTheTuan/go-shortener/pkg/database"
)

const linkCacheKeyPrefix = "link:"

// LinkCacheRepository defines cache operations for short links.
type LinkCacheRepository interface {
	Set(ctx context.Context, link *Link, ttl time.Duration) error
	Get(ctx context.Context, code string) (*Link, error)
	// Delete evicts a code's cache entry (called after a link mutation so the
	// redirect path never serves a stale/disabled/deleted target).
	Delete(ctx context.Context, code string) error
}

// cachedLink is the JSON payload stored in Redis — only the fields needed at redirect time.
type cachedLink struct {
	ID          int64  `json:"id"`
	OriginalURL string `json:"url"`
	IsActive    bool   `json:"is_active"`
}

type linkCacheRepository struct {
	rdb *database.RedisClient
}

// NewLinkCacheRepository wires a LinkCacheRepository to a Redis client.
func NewLinkCacheRepository(rdb *database.RedisClient) LinkCacheRepository {
	return &linkCacheRepository{rdb: rdb}
}

func (r *linkCacheRepository) Set(ctx context.Context, link *Link, ttl time.Duration) error {
	payload, err := json.Marshal(cachedLink{ID: link.ID, OriginalURL: link.OriginalURL, IsActive: link.IsActive})
	if err != nil {
		return err
	}
	return r.rdb.Client.Set(ctx, linkCacheKeyPrefix+link.ShortCode, payload, ttl).Err()
}

// Delete evicts the cache entry for a short code. A miss is not an error.
func (r *linkCacheRepository) Delete(ctx context.Context, code string) error {
	return r.rdb.Client.Del(ctx, linkCacheKeyPrefix+code).Err()
}

// Get returns a Link with ID and OriginalURL populated, or ErrNotFound on cache miss.
func (r *linkCacheRepository) Get(ctx context.Context, code string) (*Link, error) {
	raw, err := r.rdb.Client.Get(ctx, linkCacheKeyPrefix+code).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var cl cachedLink
	if err := json.Unmarshal([]byte(raw), &cl); err != nil {
		return nil, err
	}
	return &Link{ID: cl.ID, ShortCode: code, OriginalURL: cl.OriginalURL, IsActive: cl.IsActive}, nil
}
