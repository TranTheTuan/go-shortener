package repository

import (
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TranTheTuan/go-shortener/pkg/referrer"
	"github.com/TranTheTuan/go-shortener/pkg/useragent"
)

type dailyKey struct {
	LinkID int64
	Day    time.Time
}

type refKey struct {
	LinkID int64
	Day    time.Time
	Domain string
}

type deviceKey struct {
	LinkID  int64
	Day     time.Time
	Device  string
	Browser string
	OS      string
}

// upsertRollups aggregates clicks in memory then upserts the 3 rollup tables
// inside the provided transaction. Keys are sorted before upsert to maintain
// consistent lock ordering across pods (same rationale as the linkID sort in CreateBatch).
func upsertRollups(tx *gorm.DB, clicks []*Click) error {
	daily := map[dailyKey]int64{}
	refs := map[refKey]int64{}
	devs := map[deviceKey]int64{}

	for _, c := range clicks {
		day := c.ClickedAt.UTC().Truncate(24 * time.Hour)
		daily[dailyKey{c.LinkID, day}]++
		refs[refKey{c.LinkID, day, referrer.Domain(c.Referrer)}]++
		r := useragent.Parse(c.UserAgent)
		devs[deviceKey{c.LinkID, day, r.Device, r.Browser, r.OS}]++
	}

	if err := upsertDaily(tx, daily); err != nil {
		return err
	}
	if err := upsertReferrers(tx, refs); err != nil {
		return err
	}
	return upsertDevices(tx, devs)
}

func upsertDaily(tx *gorm.DB, m map[dailyKey]int64) error {
	if len(m) == 0 {
		return nil
	}
	keys := make([]dailyKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].LinkID != keys[j].LinkID {
			return keys[i].LinkID < keys[j].LinkID
		}
		return keys[i].Day.Before(keys[j].Day)
	})
	rows := make([]ClickStatsDaily, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, ClickStatsDaily{LinkID: k.LinkID, Day: k.Day, Clicks: m[k]})
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "link_id"}, {Name: "day"}},
		DoUpdates: clause.Assignments(map[string]any{"clicks": gorm.Expr("click_stats_daily.clicks + EXCLUDED.clicks")}),
	}).CreateInBatches(&rows, 500).Error
}

func upsertReferrers(tx *gorm.DB, m map[refKey]int64) error {
	if len(m) == 0 {
		return nil
	}
	keys := make([]refKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].LinkID != keys[j].LinkID {
			return keys[i].LinkID < keys[j].LinkID
		}
		if !keys[i].Day.Equal(keys[j].Day) {
			return keys[i].Day.Before(keys[j].Day)
		}
		return keys[i].Domain < keys[j].Domain
	})
	rows := make([]ClickStatsReferrer, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, ClickStatsReferrer{LinkID: k.LinkID, Day: k.Day, ReferrerDomain: k.Domain, Clicks: m[k]})
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "link_id"}, {Name: "day"}, {Name: "referrer_domain"}},
		DoUpdates: clause.Assignments(map[string]any{"clicks": gorm.Expr("click_stats_referrer.clicks + EXCLUDED.clicks")}),
	}).CreateInBatches(&rows, 500).Error
}

func upsertDevices(tx *gorm.DB, m map[deviceKey]int64) error {
	if len(m) == 0 {
		return nil
	}
	keys := make([]deviceKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].LinkID != keys[j].LinkID {
			return keys[i].LinkID < keys[j].LinkID
		}
		if !keys[i].Day.Equal(keys[j].Day) {
			return keys[i].Day.Before(keys[j].Day)
		}
		if keys[i].Device != keys[j].Device {
			return keys[i].Device < keys[j].Device
		}
		if keys[i].Browser != keys[j].Browser {
			return keys[i].Browser < keys[j].Browser
		}
		return keys[i].OS < keys[j].OS
	})
	rows := make([]ClickStatsDevice, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, ClickStatsDevice{LinkID: k.LinkID, Day: k.Day, Device: k.Device, Browser: k.Browser, OS: k.OS, Clicks: m[k]})
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "link_id"}, {Name: "day"}, {Name: "device"}, {Name: "browser"}, {Name: "os"}},
		DoUpdates: clause.Assignments(map[string]any{"clicks": gorm.Expr("click_stats_device.clicks + EXCLUDED.clicks")}),
	}).CreateInBatches(&rows, 500).Error
}
