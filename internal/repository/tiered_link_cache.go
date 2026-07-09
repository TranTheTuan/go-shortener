package repository

import (
	"context"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

// tieredLinkCache fronts an L2 cache (Redis) with a per-pod in-memory L1 (an
// LRU with TTL). The redirect path reads L1 first, collapsing hot-code lookups
// to RAM and sparing L2 the network round trip — Redis stops being the
// throughput ceiling for a small, hot set of codes.
//
// L1 cannot be invalidated across pods: a mutation handled by pod A evicts only
// A's L1 (and the shared L2), so pod B keeps serving its own L1 copy until it
// expires. The construction-time TTL therefore bounds staleness — a disabled,
// deleted, or re-expired link keeps redirecting from a stale L1 entry for at
// most that long. L2 remains authoritative (evicted immediately on mutation).
type tieredLinkCache struct {
	l1 *expirable.LRU[string, *Link]
	l2 LinkCacheRepository
}

// NewTieredLinkCache wraps l2 with an in-memory L1 of up to size entries, each
// living at most ttl. size <= 0 or ttl <= 0 disables L1 and returns l2 as-is.
func NewTieredLinkCache(l2 LinkCacheRepository, size int, ttl time.Duration) LinkCacheRepository {
	if size <= 0 || ttl <= 0 {
		return l2
	}
	return &tieredLinkCache{
		l1: expirable.NewLRU[string, *Link](size, nil, ttl),
		l2: l2,
	}
}

// Get checks L1 before L2 and backfills L1 on an L2 hit. Only active links ever
// reach the cache (Resolve never caches disabled ones), so an L1 hit is safe to
// serve without re-checking is_active. Misses/errors are never stored in L1.
func (t *tieredLinkCache) Get(ctx context.Context, code string) (*Link, error) {
	if link, ok := t.l1.Get(code); ok {
		return link, nil
	}
	link, err := t.l2.Get(ctx, code)
	if err != nil {
		return nil, err
	}
	t.l1.Add(code, link)
	return link, nil
}

// Set writes through to L2 (authoritative, its own long TTL) and warms L1 with
// the same link under L1's shorter construction-time TTL.
func (t *tieredLinkCache) Set(ctx context.Context, link *Link, ttl time.Duration) error {
	err := t.l2.Set(ctx, link, ttl)
	t.l1.Add(link.ShortCode, link)
	return err
}

// Delete evicts this pod's L1 entry and the shared L2 entry. Other pods' L1
// copies expire on their own TTL — the bounded-staleness trade-off.
func (t *tieredLinkCache) Delete(ctx context.Context, code string) error {
	t.l1.Remove(code)
	return t.l2.Delete(ctx, code)
}
