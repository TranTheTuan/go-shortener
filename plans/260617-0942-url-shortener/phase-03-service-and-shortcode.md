# Phase 03 — Service Layer + shortcode pkg

**Priority:** P0 · **Status:** pending · **Depends:** 02

Business logic: generate codes, create links, resolve with expiration, record + read analytics.
Also add `apperror.Gone` (410).

## Files

- Create: `pkg/shortcode/shortcode.go`
- Create: `internal/service/link_service.go`
- Create: `internal/service/analytics_service.go`
- Modify: `pkg/apperror/apperror.go` (add `Gone`)

## 1. `pkg/apperror/apperror.go` — add Gone

```go
// Gone reports a resource that existed but is no longer available (HTTP 410).
func Gone(message string) *Error {
	return New(http.StatusGone, "GONE", message)
}
```

## 2. `pkg/shortcode/shortcode.go`

Random base62 using `crypto/rand` (unpredictable per spec).

```go
const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// Generate returns a cryptographically-random base62 string of length n.
func Generate(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)] // 62 vs 256: slight modulo bias, acceptable
	}
	return string(b), nil
}
```

## 3. `internal/service/link_service.go`

```go
type CreateLinkInput struct {
	URL       string
	ExpiresAt *time.Time
}

type LinkService interface {
	Create(ctx context.Context, in CreateLinkInput) (*repository.Link, error)
	Resolve(ctx context.Context, code string) (*repository.Link, error)
}
```

`Create`:
- Trim URL; validate with `url.ParseRequestURI` AND scheme ∈ {http,https} → else `apperror.BadRequest`.
- If `ExpiresAt != nil` and `!After(now)` → `apperror.BadRequest("expires_at must be in the future")`.
- Collision retry loop (max 5):
  - `code, _ := shortcode.Generate(s.codeLen)`
  - build `Link{ShortCode: code, OriginalURL: url, ExpiresAt, CreatedAt: now}`
  - `repo.Create`; if `ErrConflict` → continue; if other err → `apperror.Internal`; else return.
- After 5 fails → `apperror.Internal(errors.New("could not generate unique code"))`.

`Resolve`:
- `repo.GetByCode`; `ErrNotFound` → `apperror.NotFound("short link not found")`.
- If `ExpiresAt != nil && ExpiresAt.Before(now)` → `apperror.Gone("short link has expired")`.
- Return link.

Constructor `NewLinkService(repo repository.LinkRepository, codeLen int)` with injected `now func() time.Time` (default `time.Now`) for testability. Guard `codeLen <= 0` → default 7.

## 4. `internal/service/analytics_service.go`

```go
type RecordInput struct {
	LinkID    int64
	Referrer  string
	IPAddress string
	UserAgent string
}
type LinkStats struct {
	ShortCode    string              `json:"short_code"`
	TotalClicks  int64               `json:"total_clicks"`
	RecentClicks []*repository.Click `json:"recent_clicks"`
}
type AnalyticsService interface {
	Record(ctx context.Context, in RecordInput) error
	Stats(ctx context.Context, code string) (*LinkStats, error)
}
```

- `Record`: build `Click{...ClickedAt: now}`, `clickRepo.Create`. Returns err (caller logs; called async).
- `Stats`: resolve link via `linkRepo.GetByCode` (NotFound→404), then `CountByLinkID` + `ListByLinkID(limit=20)`.
- Constructor takes both `LinkRepository` and `ClickRepository`.

## Todo

- [ ] Add `apperror.Gone`
- [ ] `shortcode.Generate`
- [ ] `LinkService` (Create with retry + validation, Resolve with expiry)
- [ ] `AnalyticsService` (Record, Stats)
- [ ] `go build ./...` passes

## Success Criteria

- URL/scheme + future-expiry validation enforced
- Expired link resolves to Gone; collision retried
- Services depend only on repository interfaces (mockable)

## Notes / Risks

- Modulo bias in base62 negligible for this scale (YAGNI on rejection sampling).
- Async Record may drop clicks on crash — accepted for MVP (documented in spec).
