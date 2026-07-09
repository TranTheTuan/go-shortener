package repository

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeL2 is a counting in-memory stand-in for the Redis-backed L2 cache.
type fakeL2 struct {
	store           map[string]*Link
	gets, sets, del int
}

func newFakeL2() *fakeL2 { return &fakeL2{store: map[string]*Link{}} }

func (f *fakeL2) Get(_ context.Context, code string) (*Link, error) {
	f.gets++
	if l, ok := f.store[code]; ok {
		return l, nil
	}
	return nil, ErrNotFound
}

func (f *fakeL2) Set(_ context.Context, link *Link, _ time.Duration) error {
	f.sets++
	f.store[link.ShortCode] = link
	return nil
}

func (f *fakeL2) Delete(_ context.Context, code string) error {
	f.del++
	delete(f.store, code)
	return nil
}

func TestTieredCache_L1HitSkipsL2(t *testing.T) {
	l2 := newFakeL2()
	l2.store["abc"] = &Link{ID: 1, ShortCode: "abc", OriginalURL: "https://x", IsActive: true}
	c := NewTieredLinkCache(l2, 100, time.Minute)

	// First Get: L1 miss -> L2 hit, backfills L1.
	if _, err := c.Get(context.Background(), "abc"); err != nil {
		t.Fatalf("get 1: %v", err)
	}
	// Next two Gets: served from L1, L2 untouched.
	for i := 0; i < 2; i++ {
		if _, err := c.Get(context.Background(), "abc"); err != nil {
			t.Fatalf("get %d: %v", i+2, err)
		}
	}
	if l2.gets != 1 {
		t.Fatalf("expected exactly 1 L2 Get (rest from L1), got %d", l2.gets)
	}
}

func TestTieredCache_MissNotCached(t *testing.T) {
	l2 := newFakeL2()
	c := NewTieredLinkCache(l2, 100, time.Minute)

	// A miss must NOT populate L1, so each lookup keeps hitting L2 (until Set).
	for i := 0; i < 3; i++ {
		if _, err := c.Get(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	}
	if l2.gets != 3 {
		t.Fatalf("expected 3 L2 Gets (misses not cached), got %d", l2.gets)
	}
}

func TestTieredCache_TTLExpiryRefetches(t *testing.T) {
	l2 := newFakeL2()
	l2.store["abc"] = &Link{ID: 1, ShortCode: "abc", OriginalURL: "https://x", IsActive: true}
	c := NewTieredLinkCache(l2, 100, 20*time.Millisecond)

	if _, err := c.Get(context.Background(), "abc"); err != nil { // L1 miss -> L2 (gets=1)
		t.Fatalf("get 1: %v", err)
	}
	time.Sleep(30 * time.Millisecond)                             // L1 entry expires
	if _, err := c.Get(context.Background(), "abc"); err != nil { // must re-hit L2 (gets=2)
		t.Fatalf("get 2: %v", err)
	}
	if l2.gets != 2 {
		t.Fatalf("expected 2 L2 Gets across TTL boundary, got %d", l2.gets)
	}
}

func TestTieredCache_SetWarmsL1AndDeleteEvicts(t *testing.T) {
	l2 := newFakeL2()
	c := NewTieredLinkCache(l2, 100, time.Minute)
	link := &Link{ID: 1, ShortCode: "abc", OriginalURL: "https://x", IsActive: true}

	// Set writes through to L2 and warms L1 -> subsequent Get needs no L2.
	if err := c.Set(context.Background(), link, time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := c.Get(context.Background(), "abc"); err != nil {
		t.Fatalf("get after set: %v", err)
	}
	if l2.gets != 0 {
		t.Fatalf("Set should warm L1 so Get skips L2, got %d L2 Gets", l2.gets)
	}

	// Delete evicts both tiers -> next Get falls to L2 (now empty) => NotFound.
	if err := c.Delete(context.Background(), "abc"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := c.Get(context.Background(), "abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if l2.del != 1 {
		t.Fatalf("expected 1 L2 Delete, got %d", l2.del)
	}
}

func TestTieredCache_DisabledFallsThroughToL2Passthrough(t *testing.T) {
	// size<=0 disables L1 — constructor must return the L2 as-is (no wrapper).
	l2 := newFakeL2()
	if got := NewTieredLinkCache(l2, 0, time.Minute); got != LinkCacheRepository(l2) {
		t.Fatal("size<=0 must return l2 unchanged")
	}
}
