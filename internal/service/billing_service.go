package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/PaddleHQ/paddle-go-sdk/v5/pkg/paddlenotification"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	paddlepkg "github.com/TranTheTuan/go-shortener/pkg/paddle"
)

// planOrder defines upgrade direction: basic < pro < business.
var planOrder = map[string]int{
	"basic":    0,
	"pro":      1,
	"business": 2,
}

// PaddleEvent is the raw webhook payload queued from the HTTP handler.
type PaddleEvent struct {
	EventType string `json:"event_type"`
	EventID   string `json:"event_id"`
	Raw       []byte `json:"-"` // original body for full parsing in worker
}

// BillingService handles Paddle subscription lifecycle and portal access.
type BillingService interface {
	// HandleEvent processes a verified Paddle webhook event. Idempotent.
	HandleEvent(ctx context.Context, evt PaddleEvent) error
	// CurrentPlan returns the user's active plan and subscription (or basic plan + nil sub).
	CurrentPlan(ctx context.Context, userID int64) (*repository.Plan, *repository.Subscription, error)
	// GeneratePortalURL creates a Paddle Customer Portal session URL for the given customer.
	GeneratePortalURL(ctx context.Context, paddleCustomerID string) (string, error)
}

type billingService struct {
	plans           repository.PlanRepository
	subs            repository.SubscriptionRepository
	users           repository.UserRepository
	quota           QuotaService
	sdk             paddlepkg.Client
	defaultPlanCode string
}

func NewBillingService(
	plans repository.PlanRepository,
	subs repository.SubscriptionRepository,
	users repository.UserRepository,
	quota QuotaService,
	sdk paddlepkg.Client,
	defaultPlanCode string,
) BillingService {
	return &billingService{
		plans:           plans,
		subs:            subs,
		users:           users,
		quota:           quota,
		sdk:             sdk,
		defaultPlanCode: defaultPlanCode,
	}
}

func (s *billingService) HandleEvent(ctx context.Context, evt PaddleEvent) error {
	switch paddlenotification.EventTypeName(evt.EventType) {
	case paddlenotification.EventTypeNameSubscriptionCreated:
		var e paddlenotification.SubscriptionCreated
		if err := json.Unmarshal(evt.Raw, &e); err != nil {
			return apperror.Internal(fmt.Errorf("billing: parse subscription.created: %w", err))
		}
		return s.handleSubscriptionCreated(ctx, e.Data)

	case paddlenotification.EventTypeNameSubscriptionUpdated:
		var e paddlenotification.SubscriptionUpdated
		if err := json.Unmarshal(evt.Raw, &e); err != nil {
			return apperror.Internal(fmt.Errorf("billing: parse subscription.updated: %w", err))
		}
		return s.handleSubscriptionUpdated(ctx, e.Data)

	case paddlenotification.EventTypeNameSubscriptionCanceled:
		var e paddlenotification.SubscriptionCanceled
		if err := json.Unmarshal(evt.Raw, &e); err != nil {
			return apperror.Internal(fmt.Errorf("billing: parse subscription.canceled: %w", err))
		}
		return s.handleSubscriptionCanceled(ctx, e.Data)

	case paddlenotification.EventTypeNameTransactionCompleted:
		var e paddlenotification.TransactionCompleted
		if err := json.Unmarshal(evt.Raw, &e); err != nil {
			return apperror.Internal(fmt.Errorf("billing: parse transaction.completed: %w", err))
		}
		return s.handleTransactionCompleted(ctx, e.Data)

	default:
		slog.Debug("billing: unhandled paddle event", "type", evt.EventType, "id", evt.EventID)
		return nil
	}
}

func (s *billingService) handleSubscriptionCreated(ctx context.Context, data paddlenotification.SubscriptionCreatedNotification) error {
	userID, err := s.userIDFromCustomData(data.CustomData)
	if err != nil {
		return apperror.Internal(fmt.Errorf("billing: subscription.created: %w", err))
	}

	priceID, planID, interval, err := s.resolvePlanFromItems(ctx, data.Items)
	if err != nil {
		return err
	}

	periodEnd := s.periodEndFromNotification(data.CurrentBillingPeriod)
	paddleSubID := data.ID
	paddleCustID := data.CustomerID
	status := "active"

	sub := &repository.Subscription{
		UserID:               userID,
		PlanID:               planID,
		Status:               status,
		CurrentPeriodStart:   time.Now().UTC(),
		CurrentPeriodEnd:     periodEnd,
		PaddleSubscriptionID: &paddleSubID,
		PaddleCustomerID:     &paddleCustID,
		PaddlePriceID:        &priceID,
		BillingInterval:      &interval,
	}
	if _, err := s.subs.UpsertByUserID(ctx, sub); err != nil {
		return apperror.Internal(fmt.Errorf("billing: upsert subscription: %w", err))
	}

	if err := s.users.UpdatePaddleCustomerID(ctx, userID, paddleCustID); err != nil {
		slog.Warn("billing: failed to save paddle_customer_id on user", "user_id", userID, "error", err)
	}
	return nil
}

func (s *billingService) handleSubscriptionUpdated(ctx context.Context, data paddlenotification.SubscriptionNotification) error {
	// Status=canceled means Paddle deactivated after period end — set to canceled.
	if data.Status == paddlenotification.SubscriptionStatusCanceled {
		paddleSubID := data.ID
		paddleCustID := data.CustomerID
		sub := &repository.Subscription{
			PaddleSubscriptionID: &paddleSubID,
			PaddleCustomerID:     &paddleCustID,
			Status:               "canceled",
		}
		if _, err := s.subs.UpsertByPaddleID(ctx, sub); err != nil {
			return apperror.Internal(fmt.Errorf("billing: upsert canceled subscription: %w", err))
		}
		return nil
	}

	// Check for plan upgrade (price changed).
	priceID, planID, interval, err := s.resolvePlanFromItems(ctx, data.Items)
	if err != nil {
		// price not in our catalog — ignore
		slog.Debug("billing: subscription.updated price not in catalog, ignoring", "sub_id", data.ID)
		return nil
	}

	// Detect upgrade by comparing plan order.
	existing, err := s.subs.GetActiveByUserID(ctx, s.userIDFromPaddleSubID(ctx, data.ID))
	isUpgrade := false
	if err == nil && existing != nil {
		existingPlan, perr := s.plans.GetByID(ctx, existing.PlanID)
		newPlan, nerr := s.plans.GetByID(ctx, planID)
		if perr == nil && nerr == nil {
			isUpgrade = planOrder[newPlan.Code] > planOrder[existingPlan.Code]
		}
	}

	paddleSubID := data.ID
	paddleCustID := data.CustomerID
	periodEnd := s.periodEndFromNotification(data.CurrentBillingPeriod)
	sub := &repository.Subscription{
		PaddleSubscriptionID: &paddleSubID,
		PaddleCustomerID:     &paddleCustID,
		PlanID:               planID,
		PaddlePriceID:        &priceID,
		BillingInterval:      &interval,
		Status:               "active",
		CurrentPeriodEnd:     periodEnd,
	}
	if _, err := s.subs.UpsertByPaddleID(ctx, sub); err != nil {
		return apperror.Internal(fmt.Errorf("billing: upsert updated subscription: %w", err))
	}

	if isUpgrade {
		userID := s.userIDFromPaddleSubIDViaDB(ctx, data.ID)
		if userID > 0 {
			s.quota.Reset(ctx, userID)
			slog.Info("billing: quota reset after upgrade", "user_id", userID, "plan_id", planID)
		}
	}
	return nil
}

func (s *billingService) handleSubscriptionCanceled(ctx context.Context, data paddlenotification.SubscriptionNotification) error {
	// User canceled — keep status active until period end, just set canceled_at.
	now := time.Now().UTC()
	paddleSubID := data.ID
	paddleCustID := data.CustomerID
	sub := &repository.Subscription{
		PaddleSubscriptionID: &paddleSubID,
		PaddleCustomerID:     &paddleCustID,
		CanceledAt:           &now,
		Status:               "active", // still active until period_end
	}
	if _, err := s.subs.UpsertByPaddleID(ctx, sub); err != nil {
		return apperror.Internal(fmt.Errorf("billing: upsert canceled_at: %w", err))
	}
	return nil
}

func (s *billingService) handleTransactionCompleted(ctx context.Context, data paddlenotification.TransactionNotification) error {
	if data.SubscriptionID == nil || data.BillingPeriod == nil {
		return nil // not a subscription renewal
	}
	periodEnd, err := time.Parse(time.RFC3339, data.BillingPeriod.EndsAt)
	if err != nil {
		return apperror.Internal(fmt.Errorf("billing: parse billing period end: %w", err))
	}

	paddleSubID := *data.SubscriptionID
	sub := &repository.Subscription{
		PaddleSubscriptionID: &paddleSubID,
		CurrentPeriodEnd:     &periodEnd,
		Status:               "active",
	}
	if _, err := s.subs.UpsertByPaddleID(ctx, sub); err != nil {
		return apperror.Internal(fmt.Errorf("billing: extend period_end: %w", err))
	}
	return nil
}

func (s *billingService) CurrentPlan(ctx context.Context, userID int64) (*repository.Plan, *repository.Subscription, error) {
	sub, err := s.subs.GetActiveByUserID(ctx, userID)
	if err == nil {
		plan, perr := s.plans.GetByID(ctx, sub.PlanID)
		if perr != nil {
			return nil, nil, apperror.Internal(fmt.Errorf("billing: load plan for subscription: %w", perr))
		}
		return plan, sub, nil
	}
	// No active subscription — return default plan.
	plan, err := s.plans.GetByCode(ctx, s.defaultPlanCode)
	if err != nil {
		return nil, nil, apperror.Internal(fmt.Errorf("billing: load default plan %q: %w", s.defaultPlanCode, err))
	}
	return plan, nil, nil
}

func (s *billingService) GeneratePortalURL(ctx context.Context, paddleCustomerID string) (string, error) {
	if s.sdk == nil {
		return "", apperror.Internal(fmt.Errorf("billing: paddle SDK not configured"))
	}
	url, err := s.sdk.CreatePortalSession(ctx, paddleCustomerID)
	if err != nil {
		return "", apperror.Internal(fmt.Errorf("billing: create portal session: %w", err))
	}
	return url, nil
}

// resolvePlanFromItems extracts the price ID from the first subscription item
// and looks up the matching plan in our catalog.
func (s *billingService) resolvePlanFromItems(ctx context.Context, items []paddlenotification.SubscriptionItem) (priceID string, planID int64, interval string, err error) {
	if len(items) == 0 {
		return "", 0, "", apperror.Internal(fmt.Errorf("billing: no items in subscription event"))
	}
	priceID = items[0].Price.ID
	plan, err := s.plans.GetByPaddlePriceID(ctx, priceID)
	if err != nil {
		return "", 0, "", apperror.Internal(fmt.Errorf("billing: price %q not mapped to a plan: %w", priceID, err))
	}
	interval = string(items[0].Price.BillingCycle.Interval)
	return priceID, plan.ID, interval, nil
}

// periodEndFromNotification extracts period end from a TimePeriod pointer (nil-safe).
func (s *billingService) periodEndFromNotification(p *paddlenotification.TimePeriod) *time.Time {
	if p == nil || p.EndsAt == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, p.EndsAt)
	if err != nil {
		return nil
	}
	return &t
}

// userIDFromCustomData extracts the user_id passed during Paddle Checkout.
func (s *billingService) userIDFromCustomData(data paddlenotification.CustomData) (int64, error) {
	raw, ok := data["user_id"]
	if !ok {
		return 0, fmt.Errorf("user_id missing from custom_data")
	}
	switch v := raw.(type) {
	case float64:
		return int64(v), nil
	case string:
		var id int64
		if _, err := fmt.Sscanf(v, "%d", &id); err != nil {
			return 0, fmt.Errorf("user_id not parseable: %q", v)
		}
		return id, nil
	default:
		return 0, fmt.Errorf("user_id unexpected type %T", raw)
	}
}

// userIDFromPaddleSubID looks up user_id from an existing subscription row.
// Returns 0 on any error — quota reset is best-effort.
func (s *billingService) userIDFromPaddleSubID(ctx context.Context, paddleSubID string) int64 {
	return s.userIDFromPaddleSubIDViaDB(ctx, paddleSubID)
}

func (s *billingService) userIDFromPaddleSubIDViaDB(ctx context.Context, paddleSubID string) int64 {
	sub, err := s.subs.GetByPaddleSubscriptionID(ctx, paddleSubID)
	if err != nil {
		return 0
	}
	return sub.UserID
}
