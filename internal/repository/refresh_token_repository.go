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
	// Revoke atomically marks a not-yet-revoked token as revoked. The bool
	// reports whether this call performed the transition (true) or the token
	// was already revoked / gone (false), letting callers detect a rotation
	// race or token reuse.
	Revoke(ctx context.Context, id int64) (bool, error)
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

// Revoke atomically marks the token as revoked only if it is not already
// revoked. It returns true when this call performed the transition, false when
// the token was already revoked or no longer exists. The conditional WHERE
// makes concurrent refreshes safe: only one caller can win the rotation.
func (r *refreshTokenRepository) Revoke(ctx context.Context, id int64) (bool, error) {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).
		Model(&RefreshToken{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", now)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}
