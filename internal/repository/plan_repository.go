package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// Plan is a subscription plan in the catalog. The matching SQL (and the seeded
// "basic" plan) lives in migrations/000007_create_plans_table.up.sql.
type Plan struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	Code           string    `gorm:"size:50;uniqueIndex;not null" json:"code"`
	Name           string    `gorm:"size:255;not null" json:"name"`
	DailyLinkQuota int       `gorm:"not null" json:"daily_link_quota"`
	PriceCents     int       `gorm:"not null;default:0" json:"price_cents"`
	IsActive       bool      `gorm:"not null;default:true" json:"is_active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PlanRepository defines the persistence operations for plans.
type PlanRepository interface {
	GetByCode(ctx context.Context, code string) (*Plan, error)
	GetByID(ctx context.Context, id int64) (*Plan, error)
}

// planRepository is the GORM-backed PlanRepository.
type planRepository struct {
	db *gorm.DB
}

// NewPlanRepository wires a PlanRepository to a GORM handle.
func NewPlanRepository(db *gorm.DB) PlanRepository {
	return &planRepository{db: db}
}

// GetByCode returns the plan with the given code or ErrNotFound.
func (r *planRepository) GetByCode(ctx context.Context, code string) (*Plan, error) {
	var plan Plan
	if err := r.db.WithContext(ctx).Where("code = ?", code).First(&plan).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &plan, nil
}

// GetByID returns the plan with the given ID or ErrNotFound.
func (r *planRepository) GetByID(ctx context.Context, id int64) (*Plan, error) {
	var plan Plan
	if err := r.db.WithContext(ctx).First(&plan, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &plan, nil
}
