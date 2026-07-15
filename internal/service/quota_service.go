package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/database"
	"github.com/TranTheTuan/go-shortener/pkg/redisbreaker"
)

// UnlimitedQuota is a no-op QuotaService that always allows and reports MaxInt remaining.
// Used in process roles (e.g. bulk-worker binary) that have no Redis dependency.
type UnlimitedQuota struct{}

func (UnlimitedQuota) Allow(_ context.Context, _ int64) (bool, error) { return true, nil }
func (UnlimitedQuota) Release(_ context.Context, _ int64)             {}
func (UnlimitedQuota) Reset(_ context.Context, _ int64)               {}
func (UnlimitedQuota) Remaining(_ context.Context, _ int64) int       { return math.MaxInt }

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
	// Reset sets the day's counter to 0 while preserving the key TTL.
	// Called on plan upgrade so the new higher limit takes effect immediately.
	Reset(ctx context.Context, userID int64)
	// Remaining returns slots left today. Fails open (math.MaxInt) on Redis unavailable.
	Remaining(ctx context.Context, userID int64) int
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
			if plan.DailyLinkQuota == -1 {
				return math.MaxInt // unlimited plan
			}
			return plan.DailyLinkQuota
		} else {
			slog.Warn("quota: active subscription plan lookup failed; falling back to default plan",
				"user_id", userID, "plan_id", sub.PlanID, "error", perr)
		}
	}
	if plan, err := s.plans.GetByCode(ctx, s.defaultPlanCode); err == nil {
		if plan.DailyLinkQuota == -1 {
			return math.MaxInt
		}
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

// Reset sets the day's counter to 0 while preserving the key TTL.
// Called on upgrade so the new higher limit takes effect immediately.
func (s *quotaService) Reset(ctx context.Context, userID int64) {
	_, _ = s.breaker.Do(func() (any, error) {
		// KEEPTTL preserves day-boundary TTL; only the counter resets.
		return s.rdb.Client.Set(ctx, s.key(userID), 0, redis.KeepTTL).Result()
	})
}

// Remaining returns the number of link-creation slots left today.
// Fails open (math.MaxInt) when Redis is unavailable so quota issues never
// hard-block users.
func (s *quotaService) Remaining(ctx context.Context, userID int64) int {
	limit := s.DailyLimit(ctx, userID)
	if limit == math.MaxInt {
		return math.MaxInt // unlimited plan — skip Redis lookup
	}
	res, err := s.breaker.Do(func() (any, error) {
		val, err := s.rdb.Client.Get(ctx, s.key(userID)).Int64()
		if errors.Is(err, redis.Nil) {
			return 0, nil // a miss is not a breaker failure
		}
		return val, err
	})
	if redisbreaker.IsUnavailable(err) {
		return math.MaxInt // fail open
	}
	used, _ := res.(int64)
	if r := limit - int(used); r > 0 {
		return r
	}
	return 0
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
