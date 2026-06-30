package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/TranTheTuan/go-shortener/pkg/database"
	"github.com/TranTheTuan/go-shortener/pkg/redisbreaker"
)

// DedupCache caches, per owner, the short URL already minted for a given target
// URL. The DuplicateURLCheck middleware reads it to short-circuit a repeat
// request before it reaches the quota check; LinkService writes it after a new
// create. All Redis access goes through a circuit breaker (fail-open on outage).
type DedupCache struct {
	rdb        *database.RedisClient
	breaker    *redisbreaker.Breaker
	defaultTTL time.Duration
}

// NewDedupCache wires a DedupCache to Redis. defaultTTL is used when a link has
// no explicit expiry.
func NewDedupCache(rdb *database.RedisClient, breaker *redisbreaker.Breaker, defaultTTL time.Duration) *DedupCache {
	return &DedupCache{rdb: rdb, breaker: breaker, defaultTTL: defaultTTL}
}

// key is user:links:{userID}:{sha256hex(url)}.
func (d *DedupCache) key(userID int64, url string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(url)))
	return fmt.Sprintf("user:links:%d:%s", userID, hex.EncodeToString(sum[:]))
}

// Lookup returns the owner's existing short URL for the given target URL.
// A cache miss, a breaker-open state, or any Redis error all return found=false.
func (d *DedupCache) Lookup(ctx context.Context, userID int64, url string) (string, bool) {
	res, err := d.breaker.Do(func() (any, error) {
		v, err := d.rdb.Client.Get(ctx, d.key(userID, url)).Result()
		if errors.Is(err, redis.Nil) {
			return "", nil // a miss is not a breaker failure
		}
		return v, err
	})
	if redisbreaker.IsUnavailable(err) {
		return "", false
	}
	short, _ := res.(string)
	if short == "" {
		return "", false
	}
	return short, true
}

// Remember stores the owner's short URL for a target URL. Best-effort: failures
// are swallowed (the DB dedup remains the source of truth).
func (d *DedupCache) Remember(ctx context.Context, userID int64, url, shortURL string, ttl time.Duration) {
	if ttl <= 0 {
		ttl = d.defaultTTL
	}
	if ttl <= 0 {
		return
	}
	_, _ = d.breaker.Do(func() (any, error) {
		return nil, d.rdb.Client.Set(ctx, d.key(userID, url), shortURL, ttl).Err()
	})
}
