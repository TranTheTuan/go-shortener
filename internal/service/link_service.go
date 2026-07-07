package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/metrics"
	"github.com/TranTheTuan/go-shortener/pkg/shortcode"
)

const maxCodeGenAttempts = 5
const defaultCodeLength = 7

// Pagination bounds for listing a user's links.
const (
	defaultPageLimit = 20
	maxPageLimit     = 100
)

// ClampPaging normalizes pagination params — limit to [1,100] (default 20 when
// non-positive), offset to >= 0 — so callers echo the same applied values.
func ClampPaging(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// CreateLinkInput carries the data required to create a short link.
type CreateLinkInput struct {
	URL       string
	ExpiresAt *time.Time
	// OwnerID is the authenticated creator (nil for API-key/unowned creation).
	OwnerID *int64
	// QuotaExhausted is set by the quota middleware when the owner is over their
	// daily limit. A reused (deduped) link is still returned, but creating a NEW
	// link is rejected — so reuse never counts against quota.
	QuotaExhausted bool
}

// LinkService defines the business operations for short links.
type LinkService interface {
	// Create returns the link plus whether it reused an existing one (dedup hit)
	// rather than creating a new row — the quota layer uses this to refund.
	Create(ctx context.Context, in CreateLinkInput) (*repository.Link, bool, error)
	Resolve(ctx context.Context, code string) (*repository.Link, error)
	// ListByOwner returns a page of the user's links (with click counts) and the
	// total count. status filters the set ("" = all); limit/offset are clamped.
	ListByOwner(ctx context.Context, ownerID int64, status string, limit, offset int) ([]*repository.OwnedLink, int64, error)
	// Delete removes the owner's link by code (hard delete). Returns the deleted
	// link so the caller can drop its dedup entry. NotFound if missing/non-owner.
	Delete(ctx context.Context, code string, ownerID int64) (*repository.Link, error)
	// Update replaces a link's mutable state (expiry + active) for its owner and
	// returns the updated link. NotFound if missing/non-owner.
	Update(ctx context.Context, code string, ownerID int64, expiresAt *time.Time, isActive bool) (*repository.Link, error)
}

type linkService struct {
	repo     repository.LinkRepository
	cache    repository.LinkCacheRepository // nil = caching disabled
	codeLen  int
	cacheTTL time.Duration
	now      func() time.Time
}

// NewLinkService wires a LinkService to its repository and optional cache.
// codeLen <= 0 falls back to defaultCodeLength. cache may be nil.
func NewLinkService(
	repo repository.LinkRepository,
	cache repository.LinkCacheRepository,
	codeLen int,
	cacheTTL time.Duration,
) LinkService {
	if codeLen <= 0 {
		codeLen = defaultCodeLength
	}
	return &linkService{repo: repo, cache: cache, codeLen: codeLen, cacheTTL: cacheTTL, now: time.Now}
}

// Create validates the input, deduplicates against existing links, generates a
// unique short code (retrying on collision), persists the link, and warms the cache.
func (s *linkService) Create(ctx context.Context, in CreateLinkInput) (*repository.Link, bool, error) {
	target := strings.TrimSpace(in.URL)
	if err := validateURL(target); err != nil {
		return nil, false, err
	}

	now := s.now().UTC()
	if in.ExpiresAt != nil && !in.ExpiresAt.After(now) {
		return nil, false, apperror.BadRequest("expires_at must be in the future")
	}

	// Dedup: reuse this owner's existing non-expired, ACTIVE link for the same
	// URL. A disabled link must not be reused (nor cached — Resolve serves cache
	// hits without re-checking is_active), so fall through and mint a fresh one.
	existing, err := s.repo.GetByOwnerAndURL(ctx, in.OwnerID, target)
	if err == nil {
		notExpired := existing.ExpiresAt == nil || existing.ExpiresAt.After(now)
		if notExpired && existing.IsActive {
			s.cacheSet(ctx, existing)
			return existing, true, nil // reused
		}
	}
	// ErrNotFound, expired, or disabled link → fall through to create new.

	// No existing link to reuse: a new link would be created, so enforce quota.
	if in.QuotaExhausted {
		metrics.RecordQuotaRejection(ctx)
		return nil, false, apperror.TooManyRequests("daily link quota exceeded")
	}

	for attempt := 0; attempt < maxCodeGenAttempts; attempt++ {
		code, err := shortcode.Generate(s.codeLen)
		if err != nil {
			return nil, false, apperror.Internal(fmt.Errorf("linkService.Create: %w", err))
		}
		link := &repository.Link{
			ShortCode:   code,
			OriginalURL: target,
			UserID:      in.OwnerID,
			ExpiresAt:   in.ExpiresAt,
			CreatedAt:   now,
		}
		created, err := s.repo.Create(ctx, link)
		if errors.Is(err, repository.ErrConflict) {
			continue
		}
		if err != nil {
			return nil, false, apperror.Internal(fmt.Errorf("linkService.Create: %w", err))
		}
		s.cacheSet(ctx, created)
		return created, false, nil
	}

	return nil, false, apperror.Internal(errors.New("could not generate a unique short code"))
}

// Resolve returns the link for a code using a cache-first strategy.
// On cache miss it queries the DB, checks expiry, and backfills the cache.
func (s *linkService) Resolve(ctx context.Context, code string) (*repository.Link, error) {
	if cached := s.cacheGet(ctx, code); cached != nil {
		if s.cache != nil {
			metrics.RecordCacheLookup(ctx, true)
		}
		return cached, nil
	}
	if s.cache != nil {
		metrics.RecordCacheLookup(ctx, false)
	}

	link, err := s.repo.GetByCode(ctx, code)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, apperror.NotFound("short link not found")
	}
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("linkService.Resolve: %w", err))
	}
	if link.ExpiresAt != nil && link.ExpiresAt.Before(s.now().UTC()) {
		return nil, apperror.Gone("short link has expired")
	}
	// Disabled links stop redirecting. Checked before caching so an inactive
	// link is never cached; re-enabling works on the next resolve.
	if !link.IsActive {
		// Distinct code (still 410) so redirect metrics separate disabled from expired.
		return nil, apperror.New(http.StatusGone, "DISABLED", "short link is disabled")
	}

	s.cacheSet(ctx, link)
	return link, nil
}

// ListByOwner returns a clamped page of the owner's links (with click counts)
// plus the total matching the status filter.
func (s *linkService) ListByOwner(ctx context.Context, ownerID int64, status string, limit, offset int) ([]*repository.OwnedLink, int64, error) {
	limit, offset = ClampPaging(limit, offset)
	now := s.now().UTC()

	items, err := s.repo.ListByOwner(ctx, ownerID, status, now, limit, offset)
	if err != nil {
		return nil, 0, apperror.Internal(fmt.Errorf("linkService.ListByOwner: %w", err))
	}
	total, err := s.repo.CountByOwner(ctx, ownerID, status, now)
	if err != nil {
		return nil, 0, apperror.Internal(fmt.Errorf("linkService.ListByOwner: %w", err))
	}
	return items, total, nil
}

// ownedByCode fetches a link by code and verifies the caller owns it. Missing,
// unowned, and foreign links all map to NotFound (never leak existence).
func (s *linkService) ownedByCode(ctx context.Context, code string, ownerID int64) (*repository.Link, error) {
	link, err := s.repo.GetByCode(ctx, code)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, apperror.NotFound("short link not found")
	}
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("linkService.ownedByCode: %w", err))
	}
	if link.UserID == nil || *link.UserID != ownerID {
		return nil, apperror.NotFound("short link not found")
	}
	return link, nil
}

// Delete hard-deletes the owner's link and evicts its cache entry. Returns the
// deleted link so the caller can drop the matching dedup entry.
func (s *linkService) Delete(ctx context.Context, code string, ownerID int64) (*repository.Link, error) {
	link, err := s.ownedByCode(ctx, code, ownerID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Delete(ctx, link.ID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, apperror.NotFound("short link not found")
		}
		return nil, apperror.Internal(fmt.Errorf("linkService.Delete: %w", err))
	}
	s.cacheDelete(ctx, code)
	return link, nil
}

// Update replaces a link's mutable state (expiry + active flag). A field map is
// passed so a nil expiry clears it. Evicts the cache so the redirect path
// re-reads the new state. Unlike Create, a past expires_at is allowed on
// purpose — editing the expiry to the past is a valid "expire now" (Resolve
// then returns Gone and never caches it). ponytail: intentional, no guard.
func (s *linkService) Update(ctx context.Context, code string, ownerID int64, expiresAt *time.Time, isActive bool) (*repository.Link, error) {
	link, err := s.ownedByCode(ctx, code, ownerID)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{"is_active": isActive, "expires_at": expiresAt}
	if err := s.repo.Update(ctx, link.ID, fields); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, apperror.NotFound("short link not found")
		}
		return nil, apperror.Internal(fmt.Errorf("linkService.Update: %w", err))
	}
	s.cacheDelete(ctx, code)
	link.IsActive = isActive
	link.ExpiresAt = expiresAt
	return link, nil
}

// cacheTTLFor computes the Redis TTL for a link.
// Links with ExpiresAt use the remaining duration; others use the configured default.
func (s *linkService) cacheTTLFor(link *repository.Link) time.Duration {
	if link.ExpiresAt != nil {
		ttl := time.Until(*link.ExpiresAt)
		if ttl <= 0 {
			return 0
		}
		return ttl
	}
	return s.cacheTTL
}

// cacheSet is fire-and-forget: a cache write failure never blocks the main flow.
func (s *linkService) cacheSet(ctx context.Context, link *repository.Link) {
	if s.cache == nil {
		return
	}
	ttl := s.cacheTTLFor(link)
	if ttl <= 0 {
		return
	}
	_ = s.cache.Set(ctx, link, ttl)
}

// cacheDelete evicts a code from the cache; best-effort (never blocks a mutation).
func (s *linkService) cacheDelete(ctx context.Context, code string) {
	if s.cache == nil {
		return
	}
	_ = s.cache.Delete(ctx, code)
}

// cacheGet returns the cached Link (with ID populated) or nil on any miss or error.
func (s *linkService) cacheGet(ctx context.Context, code string) *repository.Link {
	if s.cache == nil {
		return nil
	}
	link, err := s.cache.Get(ctx, code)
	if err != nil {
		return nil
	}
	return link
}

// validateURL ensures the target is a well-formed absolute http(s) URL.
func validateURL(raw string) error {
	if raw == "" {
		return apperror.BadRequest("url is required")
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return apperror.BadRequest("a valid absolute url is required")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return apperror.BadRequest("url must use http or https")
	}
	if u.Host == "" {
		return apperror.BadRequest("url must include a host")
	}
	return nil
}
