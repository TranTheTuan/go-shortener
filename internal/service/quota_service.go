package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/database"
	"github.com/TranTheTuan/go-shortener/pkg/redisbreaker"
)

// quotaKeyTTL is how long a daily quota key lives. The calendar date is part of
// the key, so this is only cleanup; it just needs to outlast the UTC day.
const quotaKeyTTL = 48 * time.Hour

// QuotaService enforces a per-user daily link-creation quota using an atomic
// Redis counter keyed by the UTC calendar day. All Redis access is behind a
// circuit breaker and fails open (allow) on any Redis trouble.
type QuotaService interface {
	// Allow atomically records one creation and reports whether the user is
	// still within their daily limit. On Redis failure it fails open (true).
	Allow(ctx context.Context, userID int64) (bool, error)
	// Release refunds one slot (e.g. the create failed or was a dedup no-op).
	Release(ctx context.Context, userID int64)
}

type quotaService struct {
	rdb             *database.RedisClient
	breaker         *redisbreaker.Breaker
	plans           repository.PlanRepository
	subs            repository.SubscriptionRepository
	defaultPlanCode string
	fallbackLimit   int
	now             func() time.Time
}

// NewQuotaService wires a QuotaService to Redis, the plan/subscription repos,
// and the default-plan settings.
func NewQuotaService(
	rdb *database.RedisClient,
	breaker *redisbreaker.Breaker,
	plans repository.PlanRepository,
	subs repository.SubscriptionRepository,
	defaultPlanCode string,
	fallbackLimit int,
) QuotaService {
	return &quotaService{
		rdb:             rdb,
		breaker:         breaker,
		plans:           plans,
		subs:            subs,
		defaultPlanCode: defaultPlanCode,
		fallbackLimit:   fallbackLimit,
		now:             time.Now,
	}
}

// key is user:quota:{userID}:{UTC-date}.
func (s *quotaService) key(userID int64) string {
	return fmt.Sprintf("user:quota:%d:%s", userID, s.now().UTC().Format("2006-01-02"))
}

// DailyLimit resolves the user's daily quota: their active subscription's plan,
// else the default plan, else the configured fallback limit.
func (s *quotaService) DailyLimit(ctx context.Context, userID int64) int {
	if sub, err := s.subs.GetActiveByUserID(ctx, userID); err == nil {
		if plan, perr := s.plans.GetByID(ctx, sub.PlanID); perr == nil {
			return plan.DailyLinkQuota
		} else {
			slog.Warn("quota: active subscription plan lookup failed; falling back to default plan",
				"user_id", userID, "plan_id", sub.PlanID, "error", perr)
		}
	}
	if plan, err := s.plans.GetByCode(ctx, s.defaultPlanCode); err == nil {
		return plan.DailyLinkQuota
	}
	slog.Warn("quota: falling back to configured limit (plan lookup failed)", "user_id", userID)
	return s.fallbackLimit
}

// Allow increments the day's counter and compares against the limit. Over the
// limit it refunds the increment (so a mid-day upgrade isn't blocked) and
// returns false. On Redis failure it logs and fails open.
func (s *quotaService) Allow(ctx context.Context, userID int64) (bool, error) {
	limit := s.DailyLimit(ctx, userID)
	key := s.key(userID)

	res, err := s.breaker.Do(func() (any, error) {
		n, err := s.rdb.Client.Incr(ctx, key).Result()
		if err != nil {
			return int64(0), err
		}
		if n == 1 {
			// Best-effort cleanup TTL; the date in the key governs correctness.
			s.rdb.Client.Expire(ctx, key, quotaKeyTTL)
		}
		return n, nil
	})
	if redisbreaker.IsUnavailable(err) {
		slog.Warn("quota check failing open (redis unavailable)", "error", err, "user_id", userID)
		return true, nil
	}

	n, _ := res.(int64)
	if int(n) > limit {
		s.decr(ctx, key) // refund: rejected attempts must not consume a slot
		return false, nil
	}
	return true, nil
}

// Release refunds one slot for the current day.
func (s *quotaService) Release(ctx context.Context, userID int64) {
	s.decr(ctx, s.key(userID))
}

func (s *quotaService) decr(ctx context.Context, key string) {
	_, _ = s.breaker.Do(func() (any, error) {
		n, err := s.rdb.Client.Decr(ctx, key).Result()
		// Floor at zero: a refund after a key reset/TTL-expiry must not drive the
		// counter negative (which would silently grant extra slots).
		if err == nil && n < 0 {
			s.rdb.Client.Set(ctx, key, 0, redis.KeepTTL)
		}
		return n, err
	})
}
