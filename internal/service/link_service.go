package service

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/shortcode"
)

const maxCodeGenAttempts = 5
const defaultCodeLength = 7

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

	// Dedup: reuse this owner's existing non-expired link for the same URL.
	existing, err := s.repo.GetByOwnerAndURL(ctx, in.OwnerID, target)
	if err == nil {
		notExpired := existing.ExpiresAt == nil || existing.ExpiresAt.After(now)
		if notExpired {
			s.cacheSet(ctx, existing)
			return existing, true, nil // reused
		}
	}
	// ErrNotFound or expired link → fall through to create new.

	// No existing link to reuse: a new link would be created, so enforce quota.
	if in.QuotaExhausted {
		return nil, false, apperror.TooManyRequests("daily link quota exceeded")
	}

	for attempt := 0; attempt < maxCodeGenAttempts; attempt++ {
		code, err := shortcode.Generate(s.codeLen)
		if err != nil {
			return nil, false, apperror.Internal(err)
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
			return nil, false, apperror.Internal(err)
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
		return cached, nil
	}

	link, err := s.repo.GetByCode(ctx, code)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, apperror.NotFound("short link not found")
	}
	if err != nil {
		return nil, apperror.Internal(err)
	}
	if link.ExpiresAt != nil && link.ExpiresAt.Before(s.now().UTC()) {
		return nil, apperror.Gone("short link has expired")
	}

	s.cacheSet(ctx, link)
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
