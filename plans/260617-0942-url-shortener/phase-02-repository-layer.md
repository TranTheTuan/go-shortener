# Phase 02 — Repository Layer

**Priority:** P0 · **Status:** pending · **Depends:** 01

GORM-backed repositories for links and clicks. Mirror `user_repository.go` patterns
(`ErrNotFound`, `ErrConflict`, `WithContext`, interface + struct).

## Files

- Create: `internal/repository/link_repository.go`
- Create: `internal/repository/click_repository.go`

`ErrNotFound` / `ErrConflict` already live in `user_repository.go` (package-level) — reuse, do NOT redefine.

## `link_repository.go`

```go
type Link struct {
	ID          int64      `gorm:"primaryKey" json:"id"`
	ShortCode   string     `gorm:"size:16;uniqueIndex;not null" json:"short_code"`
	OriginalURL string     `gorm:"not null" json:"original_url"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type LinkRepository interface {
	Create(ctx context.Context, link *Link) (*Link, error)
	GetByCode(ctx context.Context, code string) (*Link, error)
}
```

- `Create`: `db.Create`; map `gorm.ErrDuplicatedKey` → `ErrConflict` (collision retry happens in service).
- `GetByCode`: `db.Where("short_code = ?", code).First(...)`; map `gorm.ErrRecordNotFound` → `ErrNotFound`.

## `click_repository.go`

```go
type Click struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	LinkID    int64     `gorm:"index;not null" json:"link_id"`
	ClickedAt time.Time `json:"clicked_at"`
	Referrer  string    `json:"referrer,omitempty"`
	IPAddress string    `gorm:"size:45" json:"ip_address,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
}

type ClickRepository interface {
	Create(ctx context.Context, click *Click) error
	CountByLinkID(ctx context.Context, linkID int64) (int64, error)
	ListByLinkID(ctx context.Context, linkID int64, limit int) ([]*Click, error)
}
```

- `CountByLinkID`: `db.Model(&Click{}).Where("link_id = ?", id).Count(&n)`.
- `ListByLinkID`: `Where(...).Order("clicked_at DESC").Limit(limit).Find(...)`.

## Todo

- [ ] `Link` struct + `LinkRepository` (Create, GetByCode)
- [ ] `Click` struct + `ClickRepository` (Create, CountByLinkID, ListByLinkID)
- [ ] Reuse existing `ErrNotFound`/`ErrConflict`
- [ ] `go build ./...` passes

## Success Criteria

- Compiles; interfaces mockable for service tests
- GORM tags match Phase-01 SQL columns
