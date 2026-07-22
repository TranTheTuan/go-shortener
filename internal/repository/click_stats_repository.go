package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// ClickStatsDaily is the time-series rollup row: one row per (link, day).
type ClickStatsDaily struct {
	LinkID int64     `gorm:"primaryKey"`
	Day    time.Time `gorm:"primaryKey;type:date"`
	Clicks int64
}

func (ClickStatsDaily) TableName() string { return "click_stats_daily" }

// ClickStatsReferrer is the referrer rollup row.
type ClickStatsReferrer struct {
	LinkID         int64     `gorm:"primaryKey"`
	Day            time.Time `gorm:"primaryKey;type:date"`
	ReferrerDomain string    `gorm:"primaryKey;size:255"`
	Clicks         int64
}

func (ClickStatsReferrer) TableName() string { return "click_stats_referrer" }

// ClickStatsDevice is the device/browser/OS rollup row.
type ClickStatsDevice struct {
	LinkID  int64     `gorm:"primaryKey"`
	Day     time.Time `gorm:"primaryKey;type:date"`
	Device  string    `gorm:"primaryKey;size:20"`
	Browser string    `gorm:"primaryKey;size:40"`
	OS      string    `gorm:"primaryKey;size:40"`
	Clicks  int64
}

func (ClickStatsDevice) TableName() string { return "click_stats_device" }

// DailyPoint is a single timeseries data point returned by the read API.
type DailyPoint struct {
	Day    time.Time `json:"day"`
	Clicks int64     `json:"clicks"`
}

// ReferrerPoint is a single referrer data point returned by the read API.
type ReferrerPoint struct {
	Domain string `json:"domain"`
	Clicks int64  `json:"clicks"`
}

// DevicePoint is a single device breakdown data point returned by the read API.
type DevicePoint struct {
	Device  string `json:"device"`
	Browser string `json:"browser"`
	OS      string `json:"os"`
	Clicks  int64  `json:"clicks"`
}

// ClickStatsRepository provides read access to rollup tables.
// Writes happen inside CreateBatch's transaction via click_rollup_write.go.
type ClickStatsRepository interface {
	TimeseriesByLink(ctx context.Context, linkID int64, from, to time.Time) ([]DailyPoint, error)
	ReferrersByLink(ctx context.Context, linkID int64, from, to time.Time, limit int) ([]ReferrerPoint, error)
	DevicesByLink(ctx context.Context, linkID int64, from, to time.Time, limit int) ([]DevicePoint, error)
}

type clickStatsRepository struct {
	db *gorm.DB
}

// NewClickStatsRepository wires a ClickStatsRepository to a GORM handle.
func NewClickStatsRepository(db *gorm.DB) ClickStatsRepository {
	return &clickStatsRepository{db: db}
}

func (r *clickStatsRepository) TimeseriesByLink(ctx context.Context, linkID int64, from, to time.Time) ([]DailyPoint, error) {
	var rows []DailyPoint
	err := r.db.WithContext(ctx).
		Model(&ClickStatsDaily{}).
		Select("day, clicks").
		Where("link_id = ? AND day BETWEEN ? AND ?", linkID, from, to).
		Order("day ASC").
		Find(&rows).Error
	return rows, err
}

func (r *clickStatsRepository) ReferrersByLink(ctx context.Context, linkID int64, from, to time.Time, limit int) ([]ReferrerPoint, error) {
	var rows []ReferrerPoint
	err := r.db.WithContext(ctx).
		Model(&ClickStatsReferrer{}).
		Select("referrer_domain AS domain, SUM(clicks) AS clicks").
		Where("link_id = ? AND day BETWEEN ? AND ?", linkID, from, to).
		Group("referrer_domain").
		Order("clicks DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func (r *clickStatsRepository) DevicesByLink(ctx context.Context, linkID int64, from, to time.Time, limit int) ([]DevicePoint, error) {
	var rows []DevicePoint
	err := r.db.WithContext(ctx).
		Model(&ClickStatsDevice{}).
		Select("device, browser, os, SUM(clicks) AS clicks").
		Where("link_id = ? AND day BETWEEN ? AND ?", linkID, from, to).
		Group("device, browser, os").
		Order("clicks DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}
