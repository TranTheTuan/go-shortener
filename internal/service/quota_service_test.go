package service

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/database"
	"github.com/TranTheTuan/go-shortener/pkg/redisbreaker"
)

var quotaNow = time.Date(2026, 6, 26, 9, 0, 0, 0, time.UTC)

// newQuotaSvc builds a quotaService backed by a fresh miniredis + mock repos.
func newQuotaSvc(t *testing.T) (*quotaService, *miniredis.Miniredis, *mockSubRepo) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := &database.RedisClient{Client: redis.NewClient(&redis.Options{Addr: mr.Addr()})}
	plans := &mockPlanRepo{
		byCode: map[string]*repository.Plan{"basic": {ID: 1, Code: "basic", DailyLinkQuota: 10}},
		byID:   map[int64]*repository.Plan{1: {ID: 1, Code: "basic", DailyLinkQuota: 10}, 2: {ID: 2, Code: "pro", DailyLinkQuota: 100}},
	}
	subs := &mockSubRepo{active: map[int64]*repository.Subscription{}}

	svc := &quotaService{
		rdb: rdb, breaker: redisbreaker.New(10, time.Minute),
		plans: plans, subs: subs,
		defaultPlanCode: "basic", fallbackLimit: 10,
		now: func() time.Time { return quotaNow },
	}
	return svc, mr, subs
}

func TestQuota_DailyLimit_DefaultsToBasic(t *testing.T) {
	svc, _, _ := newQuotaSvc(t)
	if got := svc.DailyLimit(context.Background(), 1); got != 10 {
		t.Errorf("DailyLimit = %d, want 10 (basic fallback)", got)
	}
}

func TestQuota_DailyLimit_ActiveSubscription(t *testing.T) {
	svc, _, subs := newQuotaSvc(t)
	subs.active[1] = &repository.Subscription{UserID: 1, PlanID: 2, Status: "active"} // pro
	if got := svc.DailyLimit(context.Background(), 1); got != 100 {
		t.Errorf("DailyLimit = %d, want 100 (pro plan)", got)
	}
}

func TestQuota_Allow_UnderThenOverLimit(t *testing.T) {
	svc, _, subs := newQuotaSvc(t)
	subs.active[1] = &repository.Subscription{UserID: 1, PlanID: 0, Status: "active"} // plan 0 missing → basic (10)

	ctx := context.Background()
	for i := 1; i <= 10; i++ {
		allowed, _ := svc.Allow(ctx, 1)
		if !allowed {
			t.Fatalf("request %d should be allowed (limit 10)", i)
		}
	}
	// 11th exceeds the limit.
	if allowed, _ := svc.Allow(ctx, 1); allowed {
		t.Error("11th request should be rejected")
	}
	// Rejected attempt was refunded: the counter stays at the limit (10).
	if v := svc.rdb.Client.Get(ctx, svc.key(1)).Val(); v != "10" {
		t.Errorf("counter = %q, want 10 (rejected attempt refunded)", v)
	}
}

func TestQuota_Release_Decrements(t *testing.T) {
	svc, _, _ := newQuotaSvc(t)
	ctx := context.Background()
	_, _ = svc.Allow(ctx, 1)
	_, _ = svc.Allow(ctx, 1) // counter = 2
	svc.Release(ctx, 1)
	if v := svc.rdb.Client.Get(ctx, svc.key(1)).Val(); v != "1" {
		t.Errorf("counter = %q, want 1 after release", v)
	}
}

func TestQuota_Allow_FailsOpenWhenRedisDown(t *testing.T) {
	svc, mr, _ := newQuotaSvc(t)
	mr.Close() // simulate Redis outage

	allowed, err := svc.Allow(context.Background(), 1)
	if err != nil {
		t.Fatalf("Allow should not error on redis outage, got %v", err)
	}
	if !allowed {
		t.Error("Allow should fail open (allow) when Redis is unavailable")
	}
}
