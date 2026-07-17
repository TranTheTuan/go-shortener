# Phase 02 — Backend (Repo + Service + Handler)

**Status**: pending  
**Priority**: critical  
**Effort**: 1.5 hours  
**Blocked by**: Phase 01

## Context

Add database access layer (`UpdateTermsAccepted` method), business logic layer (`AcceptTerms` service method), HTTP handler (`AcceptTerms` endpoint), and wire the route. All following the existing patterns in the codebase.

## Implementation Steps

### 1. User Repository: Add method to interface and implementation

File: `internal/repository/user_repository.go`

**Add to struct** (after `UpdatedAt` field, around line 33):
```go
TermsAcceptedAt *time.Time `json:"terms_accepted_at,omitempty"`
TermsVersion    *string    `json:"terms_version,omitempty"`
```

**Add to interface** (after `UpdatePaddleCustomerID`, around line 50+):
```go
// UpdateTermsAccepted records terms acceptance for the user with targeted update.
// Sets both terms_accepted_at and terms_version atomically.
UpdateTermsAccepted(ctx context.Context, userID int64, version string, acceptedAt time.Time) error
```

**Add implementation** (after `UpdatePaddleCustomerID` impl, around line 100+):
```go
func (r *userRepository) UpdateTermsAccepted(ctx context.Context, userID int64, version string, acceptedAt time.Time) error {
	result := r.db.WithContext(ctx).Model(&User{}).
		Where("id = ?", userID).
		Updates(map[string]interface{}{
			"terms_accepted_at": acceptedAt,
			"terms_version":     version,
		})
	if result.Error != nil {
		return apperror.Internal(fmt.Errorf("repository: update terms accepted: %w", result.Error))
	}
	if result.RowsAffected == 0 {
		return apperror.New(http.StatusNotFound, "USER_NOT_FOUND", "user not found")
	}
	return nil
}
```

**Rationale**: Maps pattern from `UpdatePaddleCustomerID`; uses targeted `Updates(map[string]interface{})` instead of full-row `Save()` to avoid zeroing other fields.

### 2. User Service: Add method to interface and implementation

File: `internal/service/user_service.go`

**Add to interface** (after `SyncFromKeycloak`, around line 35):
```go
// AcceptTerms records user acceptance of the current T&C version.
// Returns 400 TERMS_VERSION_MISMATCH if the provided version doesn't match the current version.
AcceptTerms(ctx context.Context, userID int64, version string) error
```

**Add implementation** (after `SyncFromKeycloak` impl, around line 100+):
```go
func (s *userService) AcceptTerms(ctx context.Context, userID int64, version string) error {
	// Validate that the client is accepting the current version
	if version != s.config.Terms.CurrentVersion {
		return apperror.New(http.StatusBadRequest, "TERMS_VERSION_MISMATCH",
			fmt.Sprintf("expected version %s, got %s", s.config.Terms.CurrentVersion, version))
	}

	acceptedAt := time.Now().UTC()
	if err := s.users.UpdateTermsAccepted(ctx, userID, version, acceptedAt); err != nil {
		return err
	}

	slog.Debug("terms accepted", "user_id", userID, "version", version, "accepted_at", acceptedAt)
	return nil
}
```

**Rationale**: Version validation prevents replay attacks (client accepting an old version). Debug log for audit trail.

### 3. Auth Handler: Add handler method

File: `internal/handler/auth_handler.go`

**Add request type** (before `func (h *AuthHandler)`, around line 20):
```go
// acceptTermsRequest is the request body for POST /api/terms/accept.
type acceptTermsRequest struct {
	Version string `json:"version"`
}
```

**Add handler** (at end of AuthHandler methods):
```go
// AcceptTerms handles POST /api/terms/accept.
//
// @Summary      Accept Terms & Conditions
// @Tags         auth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      handler.acceptTermsRequest  true  "terms version"
// @Success      204
// @Failure      400  {object}  response.Envelope  "invalid version or user not found"
// @Failure      401  {object}  response.Envelope  "not authenticated"
// @Failure      500  {object}  response.Envelope
// @Router       /api/terms/accept [post]
func (h *AuthHandler) AcceptTerms(c echo.Context) error {
	userID, ok := appmw.UserIDFrom(c)
	if !ok {
		return response.Error(c, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated"))
	}

	var req acceptTermsRequest
	if err := c.Bind(&req); err != nil || req.Version == "" {
		return response.Error(c, apperror.New(http.StatusBadRequest, "BAD_REQUEST", "version is required"))
	}

	if err := h.users.AcceptTerms(c.Request().Context(), userID, req.Version); err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, http.StatusNoContent, nil)
}
```

### 4. Router: Wire the route and serve terms HTML

File: `internal/router/router.go`

**Add route** (in `registerRoutes` function, after the existing `/api/terms/*` or similar, around line 150):
```go
api.POST("/terms/accept", h.Auth.AcceptTerms)
```

**Add static terms serve** (after the `e.StaticFS("/static", ...)` line, around line 120):
```go
// Serve T&C pages
e.FileFS("/terms/:file", "terms", web.Files)
// Also serve /terms/v1.html at /terms/v1
e.FileFS("/terms/v:version", "terms/v:version.html", web.Files)
```

Actually, simpler: just serve the embed directly without fancy routing:
```go
e.GET("/terms/v*", echo.WrapHandler(http.FileServer(http.FS(web.Files))))
```

Even simpler — let Echo's static handling do it:
```go
e.StaticFS("/terms", web.Files)
```

This allows `GET /terms/v1.html` to work via the embed.

### 5. Web Embed: Ensure terms directory is included

File: `web/embed.go`

Check the current embed:
```go
//go:embed index.html error.html static
var Files embed.FS
```

Update to:
```go
//go:embed index.html error.html static terms
var Files embed.FS
```

This ensures `web/terms/v1.html` (created in Phase 03) is bundled.

## Verification

1. `go build ./...` → compiles cleanly, no missing imports
2. Run tests: `go test ./internal/repository/... -v` → `UpdateTermsAccepted` is callable
3. Run tests: `go test ./internal/service/... -v` → `AcceptTerms` validates version correctly
4. Start server: `go run ./cmd/server` → starts without error
5. Manual test (Phase 04 will do full E2E, but verify the endpoint):
   - Get auth token
   - `curl -X POST http://localhost:8000/api/terms/accept -H "Authorization: Bearer $TOKEN" -d '{"version":"1.0"}' -H "Content-Type: application/json"`
   - Expect 204 No Content
   - Query DB: `SELECT terms_accepted_at, terms_version FROM users WHERE id = ?` → should be populated

## Notes

- The version mismatch check is intentional: prevents a stale client from replaying an old accept
- All error handling follows existing patterns: `apperror.New` for client errors, `.Internal` for server errors
- Handler uses `c.Bind()` with validation (same as `Me` handler)
- Logger uses `slog.Debug` for the audit trail (non-sensitive, user-initiated action)
