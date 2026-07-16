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
	// UpgradeSubscription switches an existing subscription to a new price immediately.
	UpgradeSubscription(ctx context.Context, userID int64, priceID string) error
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
	status := s.mapSubscriptionStatus(data.Status)

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
	status := s.mapSubscriptionStatus(data.Status)

	if data.ScheduledChange != nil && data.ScheduledChange.Action == paddlenotification.ScheduledChangeActionCancel {
		paddleSubID := data.ID
		paddleCustID := data.CustomerID
		canceledAt := s.resolveCanceledAt(data)
		if canceledAt == nil {
			now := time.Now().UTC()
			canceledAt = &now
		}
		sub := &repository.Subscription{
			PaddleSubscriptionID: &paddleSubID,
			PaddleCustomerID:     &paddleCustID,
			CanceledAt:           canceledAt,
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

	paddleSubID := data.ID
	paddleCustID := data.CustomerID
	periodEnd := s.periodEndFromNotification(data.CurrentBillingPeriod)
	periodStart := s.periodStartFromNotification(data.CurrentBillingPeriod)
	canceledAt := s.resolveCanceledAt(data)

	sub := &repository.Subscription{
		PaddleSubscriptionID: &paddleSubID,
		PaddleCustomerID:     &paddleCustID,
		PlanID:               planID,
		PaddlePriceID:        &priceID,
		BillingInterval:      &interval,
		Status:               status,
		CurrentPeriodEnd:     periodEnd,
		CurrentPeriodStart:   *periodStart,
		CanceledAt:           canceledAt,
	}
	if _, err := s.subs.UpsertByPaddleID(ctx, sub); err != nil {
		return apperror.Internal(fmt.Errorf("billing: upsert updated subscription: %w", err))
	}

	if canceledAt == nil {
		if err := s.subs.ClearCanceledAt(ctx, paddleSubID); err != nil {
			return apperror.Internal(fmt.Errorf("billing: clear canceled_at: %w", err))
		}
	}

	return nil
}

func (s *billingService) handleSubscriptionCanceled(ctx context.Context, data paddlenotification.SubscriptionNotification) error {
	canceledAt := s.resolveCanceledAt(data)
	if canceledAt == nil {
		now := time.Now().UTC()
		canceledAt = &now
	}
	paddleSubID := data.ID
	paddleCustID := data.CustomerID
	sub := &repository.Subscription{
		PaddleSubscriptionID: &paddleSubID,
		PaddleCustomerID:     &paddleCustID,
		CanceledAt:           canceledAt,
		Status:               "canceled",
	}
	if _, err := s.subs.UpsertByPaddleID(ctx, sub); err != nil {
		return apperror.Internal(fmt.Errorf("billing: upsert canceled subscription: %w", err))
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

func (s *billingService) UpgradeSubscription(ctx context.Context, userID int64, priceID string) error {
	_, sub, err := s.CurrentPlan(ctx, userID)
	if err != nil {
		return err
	}
	if sub == nil || sub.PaddleSubscriptionID == nil {
		return apperror.New(404, "NO_SUBSCRIPTION", "no active subscription to upgrade")
	}
	if s.sdk == nil {
		return apperror.Internal(fmt.Errorf("billing: paddle SDK not configured"))
	}
	if err := s.sdk.UpdateSubscription(ctx, *sub.PaddleSubscriptionID, priceID); err != nil {
		return apperror.Internal(fmt.Errorf("billing: update subscription: %w", err))
	}
	return nil
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

// periodStartFromNotification extracts period start from a TimePeriod pointer (nil-safe).
func (s *billingService) periodStartFromNotification(p *paddlenotification.TimePeriod) *time.Time {
	if p == nil || p.StartsAt == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, p.StartsAt)
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

func (s *billingService) mapSubscriptionStatus(status paddlenotification.SubscriptionStatus) string {
	switch status {
	case paddlenotification.SubscriptionStatusActive, paddlenotification.SubscriptionStatusTrialing:
		return "active"
	case paddlenotification.SubscriptionStatusPaused:
		return "paused"
	case paddlenotification.SubscriptionStatusPastDue:
		return "past_due"
	default:
		return "canceled"
	}
}

func (s *billingService) resolveCanceledAt(data paddlenotification.SubscriptionNotification) *time.Time {
	if data.CanceledAt != nil && *data.CanceledAt != "" {
		if t, err := time.Parse(time.RFC3339, *data.CanceledAt); err == nil {
			return &t
		}
	}
	if data.ScheduledChange != nil && data.ScheduledChange.Action == paddlenotification.ScheduledChangeActionCancel {
		if data.ScheduledChange.EffectiveAt != "" {
			if t, err := time.Parse(time.RFC3339, data.ScheduledChange.EffectiveAt); err == nil {
				return &t
			}
		}
		now := time.Now().UTC()
		return &now
	}
	return nil
}
