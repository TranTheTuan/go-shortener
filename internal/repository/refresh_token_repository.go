package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// RefreshToken is the persisted record for an issued refresh token. Only the
// sha256 hash of the raw token is stored (TokenHash); the raw value is never
// written to the database. The matching SQL lives in
// migrations/000005_create_refresh_tokens_table.up.sql.
type RefreshToken struct {
	ID        int64  `gorm:"primaryKey"`
	UserID    int64  `gorm:"index;not null"`
	TokenHash string `gorm:"size:64;uniqueIndex;not null"`
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// RefreshTokenRepository defines the persistence operations for refresh tokens.
type RefreshTokenRepository interface {
	Create(ctx context.Context, rt *RefreshToken) (*RefreshToken, error)
	GetByHash(ctx context.Context, hash string) (*RefreshToken, error)
	Revoke(ctx context.Context, id int64) error
}

// refreshTokenRepository is the GORM-backed RefreshTokenRepository.
type refreshTokenRepository struct {
	db *gorm.DB
}

// NewRefreshTokenRepository wires a RefreshTokenRepository to a GORM handle.
func NewRefreshTokenRepository(db *gorm.DB) RefreshTokenRepository {
	return &refreshTokenRepository{db: db}
}

// Create inserts a new refresh token record and returns the stored row.
func (r *refreshTokenRepository) Create(ctx context.Context, rt *RefreshToken) (*RefreshToken, error) {
	if err := r.db.WithContext(ctx).Create(rt).Error; err != nil {
		return nil, err
	}
	return rt, nil
}

// GetByHash returns the token record for the given sha256 hash or ErrNotFound.
// The row is returned regardless of its revoked/expired state — validity is
// judged by the service layer.
func (r *refreshTokenRepository) GetByHash(ctx context.Context, hash string) (*RefreshToken, error) {
	var rt RefreshToken
	if err := r.db.WithContext(ctx).Where("token_hash = ?", hash).First(&rt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &rt, nil
}

// Revoke marks the token with the given ID as revoked. It is idempotent: a
// no-op (no error) if the row no longer exists.
func (r *refreshTokenRepository) Revoke(ctx context.Context, id int64) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&RefreshToken{}).
		Where("id = ?", id).
		Update("revoked_at", now).Error
}
