package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// Click is one recorded visit to a short link. GORM tags define the table
// schema; the matching SQL lives in migrations/000003_create_clicks_table.up.sql.
type Click struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	LinkID    int64     `gorm:"index;not null" json:"link_id"`
	ClickedAt time.Time `json:"clicked_at"`
	Referrer  string    `json:"referrer,omitempty"`
	IPAddress string    `gorm:"size:45" json:"ip_address,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
}

// ClickRepository defines the persistence operations for click events.
type ClickRepository interface {
	Create(ctx context.Context, click *Click) error
	CreateBatch(ctx context.Context, clicks []*Click) error
	CountByLinkID(ctx context.Context, linkID int64) (int64, error)
	ListByLinkID(ctx context.Context, linkID int64, limit int) ([]*Click, error)
}

// clickRepository is the GORM-backed ClickRepository.
type clickRepository struct {
	db *gorm.DB
}

// NewClickRepository wires a ClickRepository to a GORM database handle.
func NewClickRepository(db *gorm.DB) ClickRepository {
	return &clickRepository{db: db}
}

// Create inserts a single click event.
func (r *clickRepository) Create(ctx context.Context, click *Click) error {
	return r.db.WithContext(ctx).Create(click).Error
}

// CreateBatch bulk-inserts click events in chunks of 500.
func (r *clickRepository) CreateBatch(ctx context.Context, clicks []*Click) error {
	if len(clicks) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(clicks, 500).Error
}

// CountByLinkID returns the total number of clicks for a link.
func (r *clickRepository) CountByLinkID(ctx context.Context, linkID int64) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&Click{}).Where("link_id = ?", linkID).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// ListByLinkID returns the most recent clicks for a link, newest first.
func (r *clickRepository) ListByLinkID(ctx context.Context, linkID int64, limit int) ([]*Click, error) {
	var clicks []*Click
	if err := r.db.WithContext(ctx).
		Where("link_id = ?", linkID).
		Order("clicked_at DESC").
		Limit(limit).
		Find(&clicks).Error; err != nil {
		return nil, err
	}
	return clicks, nil
}
