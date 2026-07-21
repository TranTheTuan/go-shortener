package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
)

const advancedReferrerLimit = 50
const advancedDeviceLimit = 50

// AdvancedStats is the payload returned by the advanced analytics endpoint.
type AdvancedStats struct {
	ShortCode  string                     `json:"short_code"`
	Range      string                     `json:"range"`
	Timeseries []repository.DailyPoint    `json:"timeseries"`
	Referrers  []repository.ReferrerPoint `json:"referrers"`
	Devices    []repository.DevicePoint   `json:"devices"`
}

// Advanced returns advanced rollup analytics for a link, gated by entitlement.
// Non-owners receive a 404 (avoids leaking link existence). Basic-plan users receive 403.
func (s *analyticsService) Advanced(ctx context.Context, code string, userID int64, rangeStr string) (*AdvancedStats, error) {
	link, err := s.links.GetByCode(ctx, code)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, apperror.NotFound("short link not found")
	}
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("analyticsService.Advanced: %w", err))
	}

	// 404 on non-owner — do not reveal that the link exists to other users.
	if link.UserID == nil || *link.UserID != userID {
		return nil, apperror.NotFound("short link not found")
	}

	ok, err := s.entitle.HasFeature(ctx, userID, FeatureAnalyticsTimeseries)
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("analyticsService.Advanced: entitlement: %w", err))
	}
	if !ok {
		return nil, apperror.FeatureLocked("advanced analytics requires Pro or Business")
	}

	canonical, from, to := rangeWindow(rangeStr, s.now())

	ts, err := s.stats.TimeseriesByLink(ctx, link.ID, from, to)
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("analyticsService.Advanced: timeseries: %w", err))
	}
	refs, err := s.stats.ReferrersByLink(ctx, link.ID, from, to, advancedReferrerLimit)
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("analyticsService.Advanced: referrers: %w", err))
	}
	devs, err := s.stats.DevicesByLink(ctx, link.ID, from, to, advancedDeviceLimit)
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("analyticsService.Advanced: devices: %w", err))
	}

	if ts == nil {
		ts = []repository.DailyPoint{}
	}
	if refs == nil {
		refs = []repository.ReferrerPoint{}
	}
	if devs == nil {
		devs = []repository.DevicePoint{}
	}

	return &AdvancedStats{
		ShortCode:  link.ShortCode,
		Range:      canonical,
		Timeseries: ts,
		Referrers:  refs,
		Devices:    devs,
	}, nil
}

// rangeWindow converts a range string (7d/30d/90d) to UTC date boundaries.
// Unrecognised values default to 30d. Returns the canonical range string.
func rangeWindow(r string, now time.Time) (canonical string, from, to time.Time) {
	days := 30
	canonical = "30d"
	switch r {
	case "7d":
		days = 7
		canonical = "7d"
	case "90d":
		days = 90
		canonical = "90d"
	}
	to = now.UTC().Truncate(24 * time.Hour)
	from = to.AddDate(0, 0, -days)
	return canonical, from, to
}
