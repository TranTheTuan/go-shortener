# Phase 04 — Handlers + API-key Middleware + Wiring

**Priority:** P0 · **Status:** pending · **Depends:** 03

HTTP transport, API-key middleware, route registration, main wiring.

## Files

- Create: `internal/middleware/api_key.go`
- Create: `internal/handler/link_handler.go`
- Create: `internal/handler/redirect_handler.go`
- Modify: `internal/router/router.go`, `cmd/server/main.go`

## 1. `internal/middleware/api_key.go`

```go
package middleware

// APIKey returns Echo middleware that requires a matching X-API-Key header.
func APIKey(keys []string) echo.MiddlewareFunc {
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k = strings.TrimSpace(k); k != "" {
			set[k] = struct{}{}
		}
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			key := c.Request().Header.Get("X-API-Key")
			if _, ok := set[key]; key == "" || !ok {
				return response.Error(c, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid API key"))
			}
			return next(c)
		}
	}
}
```
Fail-closed: empty key set rejects all. Package name `middleware` (own dir) — import aliased in router to avoid clash with `echo/middleware`.

## 2. `internal/handler/link_handler.go`

`LinkHandler{ links service.LinkService, analytics service.AnalyticsService, baseURL string }`.

`Create` (POST /api/links):
```go
type createLinkRequest struct {
	URL       string     `json:"url"`
	ExpiresAt *time.Time `json:"expires_at"`
}
```
- Bind → BadRequest on error. Call `links.Create`. On success respond 201 with:
  `{short_code, short_url: baseURL+"/"+code, original_url, expires_at}`.

`Stats` (GET /api/links/:code/stats):
- `code := c.Param("code")`; call `analytics.Stats`; 200 with stats. Errors via `response.Error`.

## 3. `internal/handler/redirect_handler.go`

`RedirectHandler{ links service.LinkService, analytics service.AnalyticsService }`.

`Redirect` (GET /:code):
- `code := c.Param("code")`; `link, err := links.Resolve(ctx, code)`; on err `return response.Error(c, err)` (404/410/500 as mapped).
- Capture request data BEFORE goroutine (don't touch `c` async):
  ```go
  in := service.RecordInput{
  	LinkID:    link.ID,
  	Referrer:  c.Request().Referer(),
  	IPAddress: c.RealIP(),
  	UserAgent: c.Request().UserAgent(),
  }
  go func() {
  	if err := h.analytics.Record(context.Background(), in); err != nil {
  		slog.Error("record click failed", "link_id", in.LinkID, "error", err)
  	}
  }()
  ```
- `return c.Redirect(http.StatusFound, link.OriginalURL)` (302).

## 4. `internal/router/router.go`

Add to `Handlers`: `Link *handler.LinkHandler`, `Redirect *handler.RedirectHandler`.
`New` needs `apiKeys []string` (or accept a `Config`-ish param) → pass to middleware. Simplest: `New(h Handlers, apiKeys []string)`.

```go
api := e.Group("/api")
api.Use(appmw.APIKey(apiKeys))      // appmw = aliased import of internal/middleware
links := api.Group("/links")
links.POST("", h.Link.Create)
links.GET("/:code/stats", h.Link.Stats)

// public redirect — register LAST; static routes (/healthz,/api) keep priority in Echo.
e.GET("/:code", h.Redirect.Redirect)
```

## 5. `cmd/server/main.go`

Wire after db:
```go
linkRepo := repository.NewLinkRepository(db)
clickRepo := repository.NewClickRepository(db)
linkSvc := service.NewLinkService(linkRepo, cfg.Shortener.CodeLength)
analyticsSvc := service.NewAnalyticsService(linkRepo, clickRepo)

e := router.New(router.Handlers{
	Health:   handler.NewHealthHandler(),
	User:     handler.NewUserHandler(userSvc),
	Link:     handler.NewLinkHandler(linkSvc, analyticsSvc, cfg.Shortener.BaseURL),
	Redirect: handler.NewRedirectHandler(linkSvc, analyticsSvc),
}, cfg.Shortener.APIKeys)
```

## Todo

- [ ] `middleware.APIKey`
- [ ] `LinkHandler` (Create, Stats)
- [ ] `RedirectHandler` (Redirect, async click capture-before-goroutine)
- [ ] Router: `/api/links` group (api-key) + public `/:code`
- [ ] Wire in `main.go` + update `New` signature
- [ ] `go build ./...` passes

## Success Criteria

- curl create with valid key → 201 + short_url; bad/no key → 401
- visiting short_url → 302 to original; click row inserted
- stats endpoint returns total + recent
- `/healthz` still works (not shadowed by `/:code`)

## Risks

- Route ordering: `/:code` must not shadow `/healthz` or `/api/*`. Echo prioritizes static > param, so safe — verify in Phase 05.
