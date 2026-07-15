package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	. "github.com/TranTheTuan/go-shortener/internal/service"
	mocksrepository "github.com/TranTheTuan/go-shortener/internal/service/mocks/repository"
	mocksservice "github.com/TranTheTuan/go-shortener/internal/service/mocks/service"
)

// ---- raw payload builders ---------------------------------------------------

func strPtr(s string) *string { return &s }

func makeSubCreatedRaw(subID, custID, priceID, userID string) []byte {
	data := map[string]any{
		"event_type": "subscription.created",
		"event_id":   "evt_001",
		"data": map[string]any{
			"id":          subID,
			"customer_id": custID,
			"status":      "active",
			"billing_cycle": map[string]any{
				"interval":  "month",
				"frequency": 1,
			},
			"items": []any{
				map[string]any{
					"price": map[string]any{
						"id": priceID,
						"billing_cycle": map[string]any{
							"interval":  "month",
							"frequency": 1,
						},
					},
				},
			},
			"current_billing_period": map[string]any{
				"starts_at": time.Now().Format(time.RFC3339),
				"ends_at":   time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339),
			},
			"custom_data": map[string]any{
				"user_id": userID,
			},
		},
	}
	b, _ := json.Marshal(data)
	return b
}

func makeSubCanceledRaw(subID, custID string) []byte {
	data := map[string]any{
		"event_type": "subscription.canceled",
		"event_id":   "evt_003",
		"data": map[string]any{
			"id":          subID,
			"customer_id": custID,
			"status":      "active",
			"billing_cycle": map[string]any{
				"interval": "month", "frequency": 1,
			},
			"items":       []any{},
			"custom_data": map[string]any{},
		},
	}
	b, _ := json.Marshal(data)
	return b
}

func makeTxCompletedRaw(subID string) []byte {
	data := map[string]any{
		"event_type": "transaction.completed",
		"event_id":   "evt_004",
		"data": map[string]any{
			"id":              "txn_001",
			"subscription_id": subID,
			"billing_period": map[string]any{
				"starts_at": time.Now().Format(time.RFC3339),
				"ends_at":   time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339),
			},
		},
	}
	b, _ := json.Marshal(data)
	return b
}

// ---- tests ------------------------------------------------------------------

func TestBillingService_HandleEvent_SubscriptionCreated(t *testing.T) {
	ctrl := gomock.NewController(t)

	proPlan := &repository.Plan{ID: 2, Code: "pro", DailyLinkQuota: 500, PaddlePriceIDMonthly: strPtr("pri_pro_monthly")}

	plans := mocksrepository.NewMockPlanRepository(ctrl)
	plans.EXPECT().GetByPaddlePriceID(gomock.Any(), "pri_pro_monthly").Return(proPlan, nil)

	subs := mocksrepository.NewMockSubscriptionRepository(ctrl)
	subs.EXPECT().UpsertByUserID(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, sub *repository.Subscription) (*repository.Subscription, error) {
			return sub, nil
		},
	)

	users := mocksrepository.NewMockUserRepository(ctrl)
	users.EXPECT().UpdatePaddleCustomerID(gomock.Any(), int64(42), "ctm_001").Return(nil)

	quota := mocksservice.NewMockQuotaService(ctrl)

	svc := NewBillingService(plans, subs, users, quota, nil, "basic")
	raw := makeSubCreatedRaw("sub_001", "ctm_001", "pri_pro_monthly", fmt.Sprintf("%d", 42))
	if err := svc.HandleEvent(context.Background(), PaddleEvent{
		EventType: "subscription.created", EventID: "evt_001", Raw: raw,
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
}

func TestBillingService_HandleEvent_SubscriptionCanceled_SetsCanceledAt(t *testing.T) {
	ctrl := gomock.NewController(t)

	subs := mocksrepository.NewMockSubscriptionRepository(ctrl)
	subs.EXPECT().UpsertByPaddleID(gomock.Any(), gomock.AssignableToTypeOf(&repository.Subscription{})).DoAndReturn(
		func(_ context.Context, sub *repository.Subscription) (*repository.Subscription, error) {
			if sub.CanceledAt == nil {
				t.Error("canceled_at must be set")
			}
			if sub.Status != "active" {
				t.Errorf("status = %q, want active", sub.Status)
			}
			return sub, nil
		},
	)

	plans := mocksrepository.NewMockPlanRepository(ctrl)
	users := mocksrepository.NewMockUserRepository(ctrl)
	quota := mocksservice.NewMockQuotaService(ctrl)

	svc := NewBillingService(plans, subs, users, quota, nil, "basic")
	raw := makeSubCanceledRaw("sub_002", "ctm_002")
	if err := svc.HandleEvent(context.Background(), PaddleEvent{
		EventType: "subscription.canceled", EventID: "evt_003", Raw: raw,
	}); err != nil {
		t.Fatalf("HandleEvent canceled: %v", err)
	}
}

func TestBillingService_HandleEvent_TransactionCompleted_ExtendsPeriod(t *testing.T) {
	ctrl := gomock.NewController(t)

	subs := mocksrepository.NewMockSubscriptionRepository(ctrl)
	subs.EXPECT().UpsertByPaddleID(gomock.Any(), gomock.AssignableToTypeOf(&repository.Subscription{})).DoAndReturn(
		func(_ context.Context, sub *repository.Subscription) (*repository.Subscription, error) {
			if sub.CurrentPeriodEnd == nil {
				t.Error("current_period_end must be set after transaction.completed")
			}
			return sub, nil
		},
	)

	plans := mocksrepository.NewMockPlanRepository(ctrl)
	users := mocksrepository.NewMockUserRepository(ctrl)
	quota := mocksservice.NewMockQuotaService(ctrl)

	svc := NewBillingService(plans, subs, users, quota, nil, "basic")
	raw := makeTxCompletedRaw("sub_003")
	if err := svc.HandleEvent(context.Background(), PaddleEvent{
		EventType: "transaction.completed", EventID: "evt_004", Raw: raw,
	}); err != nil {
		t.Fatalf("HandleEvent tx.completed: %v", err)
	}
}

func TestBillingService_CurrentPlan_DefaultsToBasic(t *testing.T) {
	ctrl := gomock.NewController(t)

	basicPlan := &repository.Plan{ID: 1, Code: "basic", DailyLinkQuota: 10}

	subs := mocksrepository.NewMockSubscriptionRepository(ctrl)
	subs.EXPECT().GetActiveByUserID(gomock.Any(), int64(999)).Return(nil, repository.ErrNotFound)

	plans := mocksrepository.NewMockPlanRepository(ctrl)
	plans.EXPECT().GetByCode(gomock.Any(), "basic").Return(basicPlan, nil)

	users := mocksrepository.NewMockUserRepository(ctrl)
	quota := mocksservice.NewMockQuotaService(ctrl)

	svc := NewBillingService(plans, subs, users, quota, nil, "basic")
	plan, sub, err := svc.CurrentPlan(context.Background(), 999)
	if err != nil {
		t.Fatalf("CurrentPlan: %v", err)
	}
	if plan.Code != "basic" {
		t.Errorf("plan.Code = %q, want basic", plan.Code)
	}
	if sub != nil {
		t.Error("sub must be nil when on default plan")
	}
}

func TestBillingService_HandleEvent_Unknown_IsNoop(t *testing.T) {
	ctrl := gomock.NewController(t)

	// No mock expectations — unknown events touch no repos.
	plans := mocksrepository.NewMockPlanRepository(ctrl)
	subs := mocksrepository.NewMockSubscriptionRepository(ctrl)
	users := mocksrepository.NewMockUserRepository(ctrl)
	quota := mocksservice.NewMockQuotaService(ctrl)

	svc := NewBillingService(plans, subs, users, quota, nil, "basic")
	err := svc.HandleEvent(context.Background(), PaddleEvent{
		EventType: "customer.created",
		EventID:   "evt_999",
		Raw:       []byte(`{"event_type":"customer.created"}`),
	})
	if err != nil {
		t.Errorf("unknown event should be a no-op, got error: %v", err)
	}
}
