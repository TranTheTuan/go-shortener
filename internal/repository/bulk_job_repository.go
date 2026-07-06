package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

const (
	BulkJobStatusPending    = "pending"
	BulkJobStatusProcessing = "processing"
	BulkJobStatusCompleted  = "completed"
	BulkJobStatusFailed     = "failed"
)

// BulkJob represents a user-submitted batch URL shortening job.
type BulkJob struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	OwnerID   int64     `gorm:"not null" json:"owner_id"`
	FileKey   string    `gorm:"not null" json:"file_key"`
	Filename  string    `gorm:"not null" json:"filename"`
	ResultKey string    `json:"result_key,omitempty"`
	Status    string    `gorm:"size:20;not null;default:pending" json:"status"`
	TotalRows int       `gorm:"not null;default:0" json:"total_rows"`
	DoneRows  int       `gorm:"not null;default:0" json:"done_rows"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BulkJobOutbox is the transactional outbox entry for reliable Kafka delivery.
type BulkJobOutbox struct {
	ID        int64 `gorm:"primaryKey"`
	JobID     int64 `gorm:"not null"`
	Published bool  `gorm:"not null;default:false"`
	CreatedAt time.Time
}

// TableName overrides GORM's default pluralisation (bulk_job_outboxes → bulk_job_outbox).
func (BulkJobOutbox) TableName() string { return "bulk_job_outbox" }

// BulkJobRepository defines persistence operations for bulk jobs and their outbox.
type BulkJobRepository interface {
	// CreateWithOutbox inserts a BulkJob and its outbox entry atomically.
	CreateWithOutbox(ctx context.Context, job *BulkJob) (*BulkJob, error)
	GetByID(ctx context.Context, id int64) (*BulkJob, error)
	ListByOwner(ctx context.Context, ownerID int64, limit, offset int) ([]*BulkJob, error)
	UpdateStatus(ctx context.Context, id int64, status string, totalRows int) error
	UpdateResult(ctx context.Context, id int64, resultKey string, doneRows int) error
	// RelayOutbox fetches ≤50 unpublished outbox entries with FOR UPDATE SKIP LOCKED
	// and calls fn(jobID) for each; marks published on success, rolls back on error.
	// Safe to call from multiple replicas concurrently.
	RelayOutbox(ctx context.Context, fn func(jobID int64) error) error
}

type bulkJobRepository struct{ db *gorm.DB }

func NewBulkJobRepository(db *gorm.DB) BulkJobRepository {
	return &bulkJobRepository{db: db}
}

func (r *bulkJobRepository) CreateWithOutbox(ctx context.Context, job *BulkJob) (*BulkJob, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(job).Error; err != nil {
			return err
		}
		return tx.Create(&BulkJobOutbox{JobID: job.ID}).Error
	})
	if err != nil {
		return nil, err
	}
	return job, nil
}

func (r *bulkJobRepository) GetByID(ctx context.Context, id int64) (*BulkJob, error) {
	var job BulkJob
	if err := r.db.WithContext(ctx).First(&job, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &job, nil
}

func (r *bulkJobRepository) ListByOwner(ctx context.Context, ownerID int64, limit, offset int) ([]*BulkJob, error) {
	var jobs []*BulkJob
	err := r.db.WithContext(ctx).
		Where("owner_id = ?", ownerID).
		Order("created_at DESC").
		Limit(limit).Offset(offset).
		Find(&jobs).Error
	return jobs, err
}

func (r *bulkJobRepository) UpdateStatus(ctx context.Context, id int64, status string, totalRows int) error {
	return r.db.WithContext(ctx).Model(&BulkJob{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":     status,
			"total_rows": totalRows,
			"updated_at": time.Now(),
		}).Error
}

func (r *bulkJobRepository) UpdateResult(ctx context.Context, id int64, resultKey string, doneRows int) error {
	return r.db.WithContext(ctx).Model(&BulkJob{}).Where("id = ?", id).
		Updates(map[string]any{
			"result_key": resultKey,
			"done_rows":  doneRows,
			"status":     BulkJobStatusCompleted,
			"updated_at": time.Now(),
		}).Error
}

// RelayOutbox runs inside a single transaction so FOR UPDATE SKIP LOCKED holds
// until publish + mark-published complete. Kafka publish inside the tx is
// acceptable given the low frequency of bulk job confirmations.
func (r *bulkJobRepository) RelayOutbox(ctx context.Context, fn func(jobID int64) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []BulkJobOutbox
		if err := tx.Raw(
			"SELECT * FROM bulk_job_outbox WHERE published = false ORDER BY id LIMIT 50 FOR UPDATE SKIP LOCKED",
		).Scan(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			if err := fn(rows[i].JobID); err != nil {
				return err // tx rolls back; all entries retry next tick
			}
			if err := tx.Model(&BulkJobOutbox{}).Where("id = ?", rows[i].ID).
				Update("published", true).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
