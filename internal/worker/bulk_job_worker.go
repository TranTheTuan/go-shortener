package worker

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/storage"
)

const uploadTimeout = 60 * time.Second

// BulkJobWorker downloads, processes, and uploads results for a single bulk job.
type BulkJobWorker struct {
	jobs    repository.BulkJobRepository
	links   service.LinkService
	quota   service.QuotaService
	storage storage.R2Client
	baseURL string
}

func NewBulkJobWorker(jobs repository.BulkJobRepository, links service.LinkService, quota service.QuotaService, s storage.R2Client, baseURL string) *BulkJobWorker {
	return &BulkJobWorker{jobs: jobs, links: links, quota: quota, storage: s, baseURL: baseURL}
}

// Process handles one job end-to-end. Idempotent: no-op if status != pending.
func (w *BulkJobWorker) Process(ctx context.Context, jobID int64) error {
	job, err := w.jobs.GetByID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("bulk worker: get job %d: %w", jobID, err)
	}
	if job.Status != repository.BulkJobStatusPending {
		slog.Info("bulk worker: job not pending, skipping", "job_id", jobID, "status", job.Status)
		return nil
	}

	if err := w.jobs.UpdateStatus(ctx, jobID, repository.BulkJobStatusProcessing, job.TotalRows); err != nil {
		return fmt.Errorf("bulk worker: mark processing job %d: %w", jobID, err)
	}

	if err := w.process(ctx, job); err != nil {
		_ = w.jobs.UpdateStatus(ctx, jobID, repository.BulkJobStatusFailed, job.TotalRows)
		return err
	}
	return nil
}

func (w *BulkJobWorker) process(ctx context.Context, job *repository.BulkJob) error {
	// Safety net: quota may have dropped between ConfirmUpload and when this worker runs.
	// Permanent failure — return nil so Kafka commits the offset (retrying would keep failing).
	remaining := w.quota.Remaining(ctx, job.OwnerID)
	if job.TotalRows > remaining {
		_ = w.jobs.UpdateStatus(ctx, job.ID, repository.BulkJobStatusFailed, job.TotalRows)
		slog.Warn("bulk worker: quota insufficient, job failed permanently",
			"job_id", job.ID, "owner_id", job.OwnerID,
			"needed", job.TotalRows, "remaining", remaining)
		return nil
	}

	rc, err := w.storage.Download(ctx, job.FileKey)
	if err != nil {
		return fmt.Errorf("bulk worker: download job %d: %w", job.ID, err)
	}
	defer rc.Close()

	ext := filepath.Ext(job.FileKey)
	rows, err := Read(rc, ext)
	if err != nil {
		return fmt.Errorf("bulk worker: parse job %d: %w", job.ID, err)
	}

	// Collect URLs from data rows (skip header at index 0).
	urls := make([]string, len(rows)-1)
	for i := 1; i < len(rows); i++ {
		if len(rows[i]) > 0 {
			urls[i-1] = rows[i][0]
		}
	}

	links, errs := w.links.BatchCreate(ctx, job.OwnerID, urls)

	done := 0
	for i, link := range links {
		rowIdx := i + 1
		var result string
		if link != nil {
			result = w.baseURL + "/" + link.ShortCode
			done++
		} else {
			result = w.errorMsg(errs[i])
		}
		if len(rows[rowIdx]) < 2 {
			rows[rowIdx] = append(rows[rowIdx], result)
		} else {
			rows[rowIdx][1] = result
		}
	}

	buf, ct, err := Write(rows, ext)
	if err != nil {
		return fmt.Errorf("bulk worker: write result job %d: %w", job.ID, err)
	}

	resultKey := deriveResultKey(job.FileKey, ext)
	upCtx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	if err := w.storage.Upload(upCtx, resultKey, buf, int64(buf.Len()), ct); err != nil {
		return fmt.Errorf("bulk worker: upload result job %d: %w", job.ID, err)
	}

	return w.jobs.UpdateResult(ctx, job.ID, resultKey, done)
}

func (w *BulkJobWorker) errorMsg(err error) string {
	if ae, ok := apperror.As(err); ok && ae.Status == http.StatusBadRequest {
		return "url không hợp lệ"
	}
	slog.Warn("bulk worker: shorten error", "error", err)
	return "lỗi xử lý"
}

// deriveResultKey appends "-result" before the extension.
func deriveResultKey(fileKey, ext string) string {
	base := fileKey[:len(fileKey)-len(ext)]
	return base + "-result" + ext
}
