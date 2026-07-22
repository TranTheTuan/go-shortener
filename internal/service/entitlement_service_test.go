package service

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	mocksrepository "github.com/TranTheTuan/go-shortener/internal/service/mocks/repository"
)

func TestEntitlementService_HasFeature(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)

	features := mocksrepository.NewMockPlanFeatureRepository(ctrl)
	subs := mocksrepository.NewMockSubscriptionRepository(ctrl)
	plans := mocksrepository.NewMockPlanRepository(ctrl)

	svc := NewEntitlementService(features, subs, plans, "basic")

	proSub := &repository.Subscription{PlanID: 2}
	proPlan := &repository.Plan{ID: 2, Code: "pro"}
	basicPlanEnt := &repository.Plan{ID: 1, Code: "basic"}

	t.Run("pro user has timeseries", func(t *testing.T) {
		subs.EXPECT().GetActiveByUserID(gomock.Any(), int64(10)).Return(proSub, nil)
		features.EXPECT().IsEnabled(gomock.Any(), int64(2), FeatureAnalyticsTimeseries).Return(true, nil)
		ok, err := svc.HasFeature(ctx, 10, FeatureAnalyticsTimeseries)
		if err != nil || !ok {
			t.Errorf("expected true, nil; got %v, %v", ok, err)
		}
	})

	t.Run("basic user no timeseries (no active sub, default plan, row absent)", func(t *testing.T) {
		subs.EXPECT().GetActiveByUserID(gomock.Any(), int64(20)).Return(nil, repository.ErrNotFound)
		plans.EXPECT().GetByCode(gomock.Any(), "basic").Return(basicPlanEnt, nil)
		features.EXPECT().IsEnabled(gomock.Any(), int64(1), FeatureAnalyticsTimeseries).Return(false, nil)
		ok, err := svc.HasFeature(ctx, 20, FeatureAnalyticsTimeseries)
		if err != nil || ok {
			t.Errorf("expected false, nil; got %v, %v", ok, err)
		}
	})

	t.Run("unknown feature returns false", func(t *testing.T) {
		subs.EXPECT().GetActiveByUserID(gomock.Any(), int64(10)).Return(proSub, nil)
		features.EXPECT().IsEnabled(gomock.Any(), int64(2), "analytics.unknown").Return(false, nil)
		ok, err := svc.HasFeature(ctx, 10, "analytics.unknown")
		if err != nil || ok {
			t.Errorf("expected false, nil; got %v, %v", ok, err)
		}
	})

	t.Run("repo error propagated", func(t *testing.T) {
		repoErr := errors.New("db down")
		subs.EXPECT().GetActiveByUserID(gomock.Any(), int64(10)).Return(proSub, nil)
		features.EXPECT().IsEnabled(gomock.Any(), int64(2), FeatureAnalyticsTimeseries).Return(false, repoErr)
		_, err := svc.HasFeature(ctx, 10, FeatureAnalyticsTimeseries)
		if err == nil {
			t.Error("expected error to propagate")
		}
	})

	_ = proPlan // referenced via proSub.PlanID
}
