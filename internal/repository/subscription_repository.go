package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// Subscription links a user to a plan. The matching SQL lives in
// migrations/000008_create_subscriptions_table.up.sql. Absence of an active
// row means the user is on the default basic plan (resolved in the service).
type Subscription struct {
	ID                   int64      `gorm:"primaryKey" json:"id"`
	UserID               int64      `gorm:"index;not null" json:"user_id"`
	PlanID               int64      `gorm:"not null" json:"plan_id"`
	Status               string     `gorm:"size:20;not null;default:active" json:"status"`
	CurrentPeriodStart   time.Time  `json:"current_period_start"`
	CurrentPeriodEnd     *time.Time `json:"current_period_end,omitempty"`
	PaddleSubscriptionID *string    `gorm:"size:255;uniqueIndex" json:"paddle_subscription_id,omitempty"`
	PaddleCustomerID     *string    `gorm:"size:255" json:"paddle_customer_id,omitempty"`
	PaddlePriceID        *string    `gorm:"size:255" json:"paddle_price_id,omitempty"`
	BillingInterval      *string    `gorm:"size:10" json:"billing_interval,omitempty"`
	CanceledAt           *time.Time `json:"canceled_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// SubscriptionRepository defines the persistence operations for subscriptions.
type SubscriptionRepository interface {
	Create(ctx context.Context, sub *Subscription) (*Subscription, error)
	GetActiveByUserID(ctx context.Context, userID int64) (*Subscription, error)
	// Update saves all fields of the given subscription (including zero values).
	Update(ctx context.Context, sub *Subscription) error
	// GetByPaddleSubscriptionID returns the subscription with the given Paddle subscription ID.
	GetByPaddleSubscriptionID(ctx context.Context, paddleSubID string) (*Subscription, error)
	// GetByUserID returns subscription for a user.
	GetByUserID(ctx context.Context, userID int64) (*Subscription, error)
}

// subscriptionRepository is the GORM-backed SubscriptionRepository.
type subscriptionRepository struct {
	db *gorm.DB
}

// NewSubscriptionRepository wires a SubscriptionRepository to a GORM handle.
func NewSubscriptionRepository(db *gorm.DB) SubscriptionRepository {
	return &subscriptionRepository{db: db}
}

// Create inserts a new subscription and returns the stored row.
func (r *subscriptionRepository) Create(ctx context.Context, sub *Subscription) (*Subscription, error) {
	if err := r.db.WithContext(ctx).Create(sub).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, ErrConflict
		}
		return nil, err
	}
	return sub, nil
}

// GetActiveByUserID returns the user's active subscription or ErrNotFound.
func (r *subscriptionRepository) GetActiveByUserID(ctx context.Context, userID int64) (*Subscription, error) {
	var sub Subscription
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND status = ?", userID, "active").
		First(&sub).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &sub, nil
}

// Update saves all fields of the given subscription.
func (r *subscriptionRepository) Update(ctx context.Context, sub *Subscription) error {
	return r.db.WithContext(ctx).Save(sub).Error
}

// GetByPaddleSubscriptionID returns the subscription with the given Paddle subscription ID.
func (r *subscriptionRepository) GetByPaddleSubscriptionID(ctx context.Context, paddleSubID string) (*Subscription, error) {
	var sub Subscription
	if err := r.db.WithContext(ctx).
		Where("paddle_subscription_id = ?", paddleSubID).
		First(&sub).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &sub, nil
}

// GetByUserID returns all subscriptions for a user ordered by ID descending.
func (r *subscriptionRepository) GetByUserID(ctx context.Context, userID int64) (*Subscription, error) {
	var sub *Subscription
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&sub).Error; err != nil {
		return nil, err
	}
	return sub, nil
}
