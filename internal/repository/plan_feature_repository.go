package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// PlanFeature is a feature-flag row linking a plan to a capability.
type PlanFeature struct {
	ID         uint64 `gorm:"primaryKey"`
	PlanID     int64  `gorm:"not null"`
	FeatureKey string `gorm:"size:64;not null"`
	Enabled    bool   `gorm:"not null;default:true"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// PlanFeatureRepository provides read access to plan_features.
type PlanFeatureRepository interface {
	IsEnabled(ctx context.Context, planID int64, key string) (bool, error)
}

type planFeatureRepository struct {
	db *gorm.DB
}

// NewPlanFeatureRepository wires a PlanFeatureRepository to a GORM handle.
func NewPlanFeatureRepository(db *gorm.DB) PlanFeatureRepository {
	return &planFeatureRepository{db: db}
}

// IsEnabled reports whether the given feature is enabled for the plan.
// A missing row is treated as disabled (false, nil).
func (r *planFeatureRepository) IsEnabled(ctx context.Context, planID int64, key string) (bool, error) {
	var pf PlanFeature
	err := r.db.WithContext(ctx).
		Where("plan_id = ? AND feature_key = ?", planID, key).
		First(&pf).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return pf.Enabled, nil
}
