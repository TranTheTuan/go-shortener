package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
)

// recentClicksLimit caps how many recent click rows Stats returns.
const recentClicksLimit = 20

// RecordInput carries the data captured for a single redirect.
type RecordInput struct {
	LinkID    int64
	Referrer  string
	IPAddress string
	UserAgent string
}

// LinkStats is the analytics summary for a short link.
type LinkStats struct {
	ShortCode    string              `json:"short_code"`
	TotalClicks  int64               `json:"total_clicks"`
	RecentClicks []*repository.Click `json:"recent_clicks"`
}

// AnalyticsService records redirects and reports per-link statistics.
type AnalyticsService interface {
	Record(ctx context.Context, in RecordInput) error
	Stats(ctx context.Context, code string) (*LinkStats, error)
	Advanced(ctx context.Context, code string, userID int64, rangeStr string) (*AdvancedStats, error)
}

// analyticsService is the default AnalyticsService backed by the link and click
// repositories.
type analyticsService struct {
	links   repository.LinkRepository
	clicks  repository.ClickRepository
	stats   repository.ClickStatsRepository
	entitle EntitlementService
	now     func() time.Time
}

// NewAnalyticsService wires an AnalyticsService to its repositories.
func NewAnalyticsService(
	links repository.LinkRepository,
	clicks repository.ClickRepository,
	stats repository.ClickStatsRepository,
	entitle EntitlementService,
) AnalyticsService {
	return &analyticsService{
		links:   links,
		clicks:  clicks,
		stats:   stats,
		entitle: entitle,
		now:     time.Now,
	}
}

// Record persists a click event. It is typically called asynchronously from the
// redirect handler, so it returns its error for the caller to log rather than
// surface to the visitor.
func (s *analyticsService) Record(ctx context.Context, in RecordInput) error {
	return s.clicks.Create(ctx, &repository.Click{
		LinkID:    in.LinkID,
		ClickedAt: s.now().UTC(),
		Referrer:  in.Referrer,
		IPAddress: in.IPAddress,
		UserAgent: in.UserAgent,
	})
}

// Stats returns the total and most-recent clicks for the link with the given
// code, mapping an unknown code to a not-found application error.
func (s *analyticsService) Stats(ctx context.Context, code string) (*LinkStats, error) {
	link, err := s.links.GetByCode(ctx, code)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, apperror.NotFound("short link not found")
	}
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("analyticsService.Stats: %w", err))
	}

	total, err := s.clicks.CountByLinkID(ctx, link.ID)
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("analyticsService.Stats: %w", err))
	}

	recent, err := s.clicks.ListByLinkID(ctx, link.ID, recentClicksLimit)
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("analyticsService.Stats: %w", err))
	}

	return &LinkStats{
		ShortCode:    link.ShortCode,
		TotalClicks:  total,
		RecentClicks: recent,
	}, nil
}
