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

// maxCodeGenAttempts bounds the collision-retry loop when generating a unique
// short code.
const maxCodeGenAttempts = 5

// defaultCodeLength is used when the configured length is non-positive.
const defaultCodeLength = 7

// CreateLinkInput carries the data required to create a short link.
type CreateLinkInput struct {
	URL       string
	ExpiresAt *time.Time
}

// LinkService defines the business operations for short links.
type LinkService interface {
	Create(ctx context.Context, in CreateLinkInput) (*repository.Link, error)
	Resolve(ctx context.Context, code string) (*repository.Link, error)
}

// linkService is the default LinkService backed by a LinkRepository.
type linkService struct {
	repo    repository.LinkRepository
	codeLen int
	now     func() time.Time
}

// NewLinkService wires a LinkService to its repository. codeLen <= 0 falls back
// to the default length.
func NewLinkService(repo repository.LinkRepository, codeLen int) LinkService {
	if codeLen <= 0 {
		codeLen = defaultCodeLength
	}
	return &linkService{
		repo:    repo,
		codeLen: codeLen,
		now:     time.Now,
	}
}

// Create validates the input, generates a unique short code (retrying on
// collision), and persists the link.
func (s *linkService) Create(ctx context.Context, in CreateLinkInput) (*repository.Link, error) {
	target := strings.TrimSpace(in.URL)
	if err := validateURL(target); err != nil {
		return nil, err
	}

	now := s.now().UTC()
	if in.ExpiresAt != nil && !in.ExpiresAt.After(now) {
		return nil, apperror.BadRequest("expires_at must be in the future")
	}

	for attempt := 0; attempt < maxCodeGenAttempts; attempt++ {
		code, err := shortcode.Generate(s.codeLen)
		if err != nil {
			return nil, apperror.Internal(err)
		}

		link := &repository.Link{
			ShortCode:   code,
			OriginalURL: target,
			ExpiresAt:   in.ExpiresAt,
			CreatedAt:   now,
		}

		created, err := s.repo.Create(ctx, link)
		if errors.Is(err, repository.ErrConflict) {
			continue // collision — try a fresh code
		}
		if err != nil {
			return nil, apperror.Internal(err)
		}
		return created, nil
	}

	return nil, apperror.Internal(errors.New("could not generate a unique short code"))
}

// Resolve returns the link for a code, mapping not-found and expired states to
// application errors.
func (s *linkService) Resolve(ctx context.Context, code string) (*repository.Link, error) {
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
	return link, nil
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
