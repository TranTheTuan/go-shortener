package service

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/TranTheTuan/go-shortener/pkg/database"
	"github.com/TranTheTuan/go-shortener/pkg/redisbreaker"
)

func newDedupCache(t *testing.T) (*DedupCache, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := &database.RedisClient{Client: redis.NewClient(&redis.Options{Addr: mr.Addr()})}
	return NewDedupCache(rdb, redisbreaker.New(10, time.Minute), time.Hour), mr
}

func TestDedupCache_RememberThenLookup(t *testing.T) {
	d, _ := newDedupCache(t)
	ctx := context.Background()

	if _, found := d.Lookup(ctx, 1, "https://example.com"); found {
		t.Error("expected miss before Remember")
	}

	d.Remember(ctx, 1, "https://example.com", "http://sho.rt/abc", time.Minute)

	short, found := d.Lookup(ctx, 1, "https://example.com")
	if !found || short != "http://sho.rt/abc" {
		t.Errorf("Lookup = (%q, %v), want (http://sho.rt/abc, true)", short, found)
	}

	// Scoped per owner: a different user does not see it.
	if _, found := d.Lookup(ctx, 2, "https://example.com"); found {
		t.Error("dedup cache must be per-owner")
	}
}

func TestDedupCache_FailsClosedAsMissWhenRedisDown(t *testing.T) {
	d, mr := newDedupCache(t)
	mr.Close()
	if _, found := d.Lookup(context.Background(), 1, "https://example.com"); found {
		t.Error("Lookup should report a miss when Redis is unavailable")
	}
}
