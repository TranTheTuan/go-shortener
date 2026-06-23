# Phase 02 — Repository Layer

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260622-1539-username-password-auth.md)

## Overview
- **Priority:** High
- **Status:** pending
- Extend `User` with credentials + lookups; add `RefreshTokenRepository`.

## Related Code Files
- **Modify:** `internal/repository/user_repository.go`
- **Create:** `internal/repository/refresh_token_repository.go`

## Implementation Steps

1. **`user_repository.go`** — extend `User` struct:
   ```go
   type User struct {
       ID           int64     `gorm:"primaryKey" json:"id"`
       Username     string    `gorm:"size:255;uniqueIndex;not null" json:"username"`
       Email        string    `gorm:"size:255;uniqueIndex;not null" json:"email"`
       PasswordHash string    `gorm:"size:255;not null" json:"-"` // never serialized
       Name         *string   `gorm:"size:255" json:"name,omitempty"`
       CreatedAt    time.Time `json:"created_at"`
       UpdatedAt    time.Time `json:"updated_at"`
   }
   ```
   Add to `UserRepository` interface + impl:
   ```go
   GetByEmail(ctx context.Context, email string) (*User, error)
   GetByUsername(ctx context.Context, username string) (*User, error)
   ```
   Each: `r.db.WithContext(ctx).Where("email = ?", email).First(&u)`, map
   `gorm.ErrRecordNotFound` → `ErrNotFound`. `Create` already maps duplicate → `ErrConflict`.

2. **`refresh_token_repository.go`** — new file:
   ```go
   type RefreshToken struct {
       ID        int64      `gorm:"primaryKey"`
       UserID    int64      `gorm:"index;not null"`
       TokenHash string     `gorm:"size:64;uniqueIndex;not null"`
       ExpiresAt time.Time
       RevokedAt *time.Time
       CreatedAt time.Time
   }

   type RefreshTokenRepository interface {
       Create(ctx context.Context, rt *RefreshToken) (*RefreshToken, error)
       GetByHash(ctx context.Context, hash string) (*RefreshToken, error) // ErrNotFound if absent
       Revoke(ctx context.Context, id int64) error                        // sets revoked_at = now
   }
   ```
   GORM-backed impl `refreshTokenRepository{db}` + `NewRefreshTokenRepository(db)`.
   `Revoke`: `Update("revoked_at", time.Now().UTC())` on the row; no error if 0 rows.

3. `go build ./...`.

## Key Insight
Repo stores only the **hash**; raw refresh token never persisted. `GetByHash`
returns the row regardless of revoked/expired state — validity is judged in the
service layer (so it can distinguish expired vs revoked vs unknown).

## Todo
- [ ] Extend `User` struct (username, password_hash json:"-", name nullable)
- [ ] `GetByEmail` + `GetByUsername`
- [ ] `RefreshToken` entity + repository + constructor
- [ ] `go build ./...` passes

## Success Criteria
- Compiles. Interfaces match what Phase 03 service expects.

## Next
Phase 03 consumes these repositories.
