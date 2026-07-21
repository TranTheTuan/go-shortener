package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/TranTheTuan/go-shortener/internal/repository"
)

// Feature key constants shared with the migration seed and the analytics handler.
const (
	FeatureAnalyticsTimeseries = "analytics.timeseries"
	FeatureAnalyticsReferrers  = "analytics.referrers"
	FeatureAnalyticsDevices    = "analytics.devices"
)

// EntitlementService checks whether a user's plan enables a given feature.
type EntitlementService interface {
	HasFeature(ctx context.Context, userID int64, key string) (bool, error)
}

type entitlementService struct {
	features        repository.PlanFeatureRepository
	subs            repository.SubscriptionRepository
	plans           repository.PlanRepository
	defaultPlanCode string
}

// NewEntitlementService wires an EntitlementService to its repositories.
func NewEntitlementService(
	features repository.PlanFeatureRepository,
	subs repository.SubscriptionRepository,
	plans repository.PlanRepository,
	defaultPlanCode string,
) EntitlementService {
	return &entitlementService{
		features:        features,
		subs:            subs,
		plans:           plans,
		defaultPlanCode: defaultPlanCode,
	}
}

// HasFeature resolves the user's active plan then checks the feature flag.
// Resolution mirrors quotaService.MonthlyLimit: active sub → default plan.
func (s *entitlementService) HasFeature(ctx context.Context, userID int64, key string) (bool, error) {
	planID, err := s.resolvePlanID(ctx, userID)
	if err != nil {
		return false, err
	}
	return s.features.IsEnabled(ctx, planID, key)
}

func (s *entitlementService) resolvePlanID(ctx context.Context, userID int64) (int64, error) {
	sub, err := s.subs.GetActiveByUserID(ctx, userID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return 0, err // propagate real DB errors; don't silently degrade to basic
	}
	if err == nil {
		return sub.PlanID, nil
	}
	// No active subscription — fall back to the default plan (basic).
	plan, err := s.plans.GetByCode(ctx, s.defaultPlanCode)
	if err != nil {
		slog.Warn("entitlement: default plan lookup failed", "code", s.defaultPlanCode, "error", err)
		return 0, err
	}
	return plan.ID, nil
}
