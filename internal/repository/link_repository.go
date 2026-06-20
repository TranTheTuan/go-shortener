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
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// LinkRepository defines the persistence operations for short links.
type LinkRepository interface {
	Create(ctx context.Context, link *Link) (*Link, error)
	GetByCode(ctx context.Context, code string) (*Link, error)
	GetByOriginalURL(ctx context.Context, url string) (*Link, error)
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

// GetByOriginalURL returns the link for the given original URL or ErrNotFound.
func (r *linkRepository) GetByOriginalURL(ctx context.Context, url string) (*Link, error) {
	var link Link
	if err := r.db.WithContext(ctx).Where("original_url = ?", url).First(&link).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &link, nil
}
