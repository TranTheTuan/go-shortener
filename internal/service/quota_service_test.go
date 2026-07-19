package service

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/mock/gomock"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	mocksrepository "github.com/TranTheTuan/go-shortener/internal/service/mocks/repository"
	"github.com/TranTheTuan/go-shortener/pkg/database"
	"github.com/TranTheTuan/go-shortener/pkg/redisbreaker"
)

var quotaNow = time.Date(2026, 6, 26, 9, 0, 0, 0, time.UTC)

var (
	basicPlan = &repository.Plan{ID: 1, Code: "basic", MonthlyLinkQuota: 300}
	proPlan   = &repository.Plan{ID: 2, Code: "pro", MonthlyLinkQuota: 15000}
)

// newQuotaSvc builds a quotaService backed by a fresh miniredis + gomock repos.
func newQuotaSvc(t *testing.T, ctrl *gomock.Controller) (*quotaService, *miniredis.Miniredis, *mocksrepository.MockPlanRepository, *mocksrepository.MockSubscriptionRepository) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := &database.RedisClient{Client: redis.NewClient(&redis.Options{Addr: mr.Addr()})}
	plans := mocksrepository.NewMockPlanRepository(ctrl)
	subs := mocksrepository.NewMockSubscriptionRepository(ctrl)

	svc := &quotaService{
		rdb: rdb, breaker: redisbreaker.New(10, time.Minute),
		plans: plans, subs: subs,
		defaultPlanCode: "basic", fallbackLimit: 10,
		now: func() time.Time { return quotaNow },
	}
	return svc, mr, plans, subs
}

func TestQuota_MonthlyLimit_DefaultsToBasic(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc, _, plans, subs := newQuotaSvc(t, ctrl)

	subs.EXPECT().GetActiveByUserID(gomock.Any(), int64(1)).Return(nil, repository.ErrNotFound)
	plans.EXPECT().GetByCode(gomock.Any(), "basic").Return(basicPlan, nil)

	if got := svc.MonthlyLimit(context.Background(), 1); got != 300 {
		t.Errorf("MonthlyLimit = %d, want 300 (basic fallback)", got)
	}
}

func TestQuota_MonthlyLimit_ActiveSubscription(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc, _, plans, subs := newQuotaSvc(t, ctrl)

	sub := &repository.Subscription{UserID: 1, PlanID: 2, Status: "active"}
	subs.EXPECT().GetActiveByUserID(gomock.Any(), int64(1)).Return(sub, nil)
	plans.EXPECT().GetByID(gomock.Any(), int64(2)).Return(proPlan, nil)

	if got := svc.MonthlyLimit(context.Background(), 1); got != 15000 {
		t.Errorf("MonthlyLimit = %d, want 15000 (pro plan)", got)
	}
}

func TestQuota_Allow_UnderThenOverLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc, _, plans, subs := newQuotaSvc(t, ctrl)

	// plan 0 not found → falls back to basic (300)
	sub := &repository.Subscription{UserID: 1, PlanID: 0, Status: "active"}
	subs.EXPECT().GetActiveByUserID(gomock.Any(), int64(1)).Return(sub, nil).AnyTimes()
	plans.EXPECT().GetByID(gomock.Any(), int64(0)).Return(nil, repository.ErrNotFound).AnyTimes()
	plans.EXPECT().GetByCode(gomock.Any(), "basic").Return(basicPlan, nil).AnyTimes()

	ctx := context.Background()
	for i := 1; i <= 300; i++ {
		allowed, _ := svc.Allow(ctx, 1)
		if !allowed {
			t.Fatalf("request %d should be allowed (limit 300)", i)
		}
	}
	// 301st exceeds the limit.
	if allowed, _ := svc.Allow(ctx, 1); allowed {
		t.Error("301st request should be rejected")
	}
	// Rejected attempt was refunded: the counter stays at the limit (300).
	if v := svc.rdb.Client.Get(ctx, svc.key(1)).Val(); v != "300" {
		t.Errorf("counter = %q, want 300 (rejected attempt refunded)", v)
	}
}

func TestQuota_Release_Decrements(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc, _, plans, subs := newQuotaSvc(t, ctrl)

	subs.EXPECT().GetActiveByUserID(gomock.Any(), int64(1)).Return(nil, repository.ErrNotFound).AnyTimes()
	plans.EXPECT().GetByCode(gomock.Any(), "basic").Return(basicPlan, nil).AnyTimes()

	ctx := context.Background()
	_, _ = svc.Allow(ctx, 1)
	_, _ = svc.Allow(ctx, 1) // counter = 2
	svc.Release(ctx, 1)
	if v := svc.rdb.Client.Get(ctx, svc.key(1)).Val(); v != "1" {
		t.Errorf("counter = %q, want 1 after release", v)
	}
}

func TestQuota_Allow_FailsOpenWhenRedisDown(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc, mr, plans, subs := newQuotaSvc(t, ctrl)

	subs.EXPECT().GetActiveByUserID(gomock.Any(), int64(1)).Return(nil, repository.ErrNotFound)
	plans.EXPECT().GetByCode(gomock.Any(), "basic").Return(basicPlan, nil)

	mr.Close() // simulate Redis outage

	allowed, err := svc.Allow(context.Background(), 1)
	if err != nil {
		t.Fatalf("Allow should not error on redis outage, got %v", err)
	}
	if !allowed {
		t.Error("Allow should fail open (allow) when Redis is unavailable")
	}
}
