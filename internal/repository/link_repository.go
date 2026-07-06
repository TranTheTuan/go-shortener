package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// Link is the domain entity for a shortened URL. GORM tags define the table
// schema; the matching SQL lives in migrations/000002_create_links_table.up.sql.
type Link struct {
	ID          int64      `gorm:"primaryKey" json:"id"`
	ShortCode   string     `gorm:"size:16;uniqueIndex;not null" json:"short_code"`
	OriginalURL string     `gorm:"not null" json:"original_url"`
	UserID      *int64     `gorm:"index" json:"user_id,omitempty"` // nil = created via API key (unowned)
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	IsActive    bool       `gorm:"not null;default:true" json:"is_active"`
	CreatedAt   time.Time  `json:"created_at"`
}

// OwnedLink is a link plus its click count, used by the owner's list view.
type OwnedLink struct {
	Link
	TotalClicks int64 `json:"total_clicks"`
}

// Link status filters for the owner's list (mutually exclusive; "" = all).
const (
	LinkStatusActive   = "active"
	LinkStatusDisabled = "disabled"
	LinkStatusExpired  = "expired"
)

// LinkRepository defines the persistence operations for short links.
type LinkRepository interface {
	Create(ctx context.Context, link *Link) (*Link, error)
	GetByCode(ctx context.Context, code string) (*Link, error)
	// GetByOwnerAndURL finds a link for the given URL scoped to one owner.
	// ownerID nil matches the unowned (API-key) group.
	GetByOwnerAndURL(ctx context.Context, ownerID *int64, url string) (*Link, error)
	// ListByOwner returns the owner's links (newest first) with their click
	// counts, paginated by limit/offset. status filters the set (see the
	// LinkStatus* constants; "" = all); now anchors the expiry comparison.
	ListByOwner(ctx context.Context, ownerID int64, status string, now time.Time, limit, offset int) ([]*OwnedLink, error)
	// CountByOwner returns the number of links owned by the user matching the
	// same status filter as ListByOwner.
	CountByOwner(ctx context.Context, ownerID int64, status string, now time.Time) (int64, error)
	// Delete hard-deletes a link by ID (clicks cascade via FK). ErrNotFound if absent.
	Delete(ctx context.Context, id int64) error
	// Update writes the given columns for a link by ID. A field map (not a
	// struct) so nil/false values are written. ErrNotFound if absent.
	Update(ctx context.Context, id int64, fields map[string]any) error
}

// applyLinkStatus adds the status predicate (relative to now) to a links query.
// Buckets are mutually exclusive and mirror the UI badge precedence
// (disabled > expired > active). Unknown/empty status = no predicate (all).
func applyLinkStatus(q *gorm.DB, status string, now time.Time) *gorm.DB {
	switch status {
	case LinkStatusActive:
		return q.Where("links.is_active AND (links.expires_at IS NULL OR links.expires_at > ?)", now)
	case LinkStatusDisabled:
		return q.Where("NOT links.is_active")
	case LinkStatusExpired:
		return q.Where("links.is_active AND links.expires_at IS NOT NULL AND links.expires_at <= ?", now)
	default:
		return q
	}
}

// linkRepository is the GORM-backed LinkRepository.
type linkRepository struct {
	db *gorm.DB
}

// NewLinkRepository wires a LinkRepository to a GORM database handle.
func NewLinkRepository(db *gorm.DB) LinkRepository {
	return &linkRepository{db: db}
}

// Create inserts a new link. A unique-constraint violation on short_code is
// reported as ErrConflict so the service can retry with a fresh code.
func (r *linkRepository) Create(ctx context.Context, link *Link) (*Link, error) {
	if err := r.db.WithContext(ctx).Create(link).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, ErrConflict
		}
		return nil, err
	}
	return link, nil
}

// GetByCode returns the link for the given short code or ErrNotFound.
func (r *linkRepository) GetByCode(ctx context.Context, code string) (*Link, error) {
	var link Link
	if err := r.db.WithContext(ctx).Where("short_code = ?", code).First(&link).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &link, nil
}

// ListByOwner returns the owner's links, newest first, each with its click
// count (LEFT JOIN so click-less links report 0), paginated by limit/offset.
func (r *linkRepository) ListByOwner(ctx context.Context, ownerID int64, status string, now time.Time, limit, offset int) ([]*OwnedLink, error) {
	var out []*OwnedLink
	q := r.db.WithContext(ctx).
		Model(&Link{}).
		Select("links.*, COUNT(clicks.id) AS total_clicks").
		Joins("LEFT JOIN clicks ON clicks.link_id = links.id").
		Where("links.user_id = ?", ownerID)
	q = applyLinkStatus(q, status, now)
	err := q.
		Group("links.id").
		Order("links.created_at DESC").
		Limit(limit).Offset(offset).
		Scan(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CountByOwner returns the number of links owned by the user matching status.
func (r *linkRepository) CountByOwner(ctx context.Context, ownerID int64, status string, now time.Time) (int64, error) {
	var n int64
	q := r.db.WithContext(ctx).Model(&Link{}).Where("links.user_id = ?", ownerID)
	q = applyLinkStatus(q, status, now)
	err := q.Count(&n).Error
	return n, err
}

// Delete hard-deletes a link by ID; clicks cascade via the FK. ErrNotFound if absent.
func (r *linkRepository) Delete(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&Link{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Update writes the given columns for a link by ID. A field map is used (not a
// struct) so zero/nil values — clearing expires_at, setting is_active=false —
// are persisted rather than skipped. ErrNotFound if the row is absent.
func (r *linkRepository) Update(ctx context.Context, id int64, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&Link{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// GetByOwnerAndURL returns the owner's link for the given original URL, or
// ErrNotFound. A nil ownerID matches the unowned (API-key) group.
func (r *linkRepository) GetByOwnerAndURL(ctx context.Context, ownerID *int64, url string) (*Link, error) {
	q := r.db.WithContext(ctx)
	if ownerID == nil {
		q = q.Where("user_id IS NULL")
	} else {
		q = q.Where("user_id = ?", *ownerID)
	}

	var link Link
	if err := q.Where("original_url = ?", url).First(&link).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &link, nil
}
