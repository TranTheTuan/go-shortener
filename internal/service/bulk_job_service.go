package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/storage"
)

const (
	maxBulkRows    = 10_000
	presignPutTTL  = 15 * time.Minute
	resultGetTTL   = time.Hour
)

// BulkJobService handles presign, confirm, get, list, and template operations.
type BulkJobService interface {
	GetUploadURL(ctx context.Context, ownerID int64, filename, contentMD5 string, rowCount int) (presignedURL, fileKey string, err error)
	ConfirmUpload(ctx context.Context, ownerID int64, fileKey, filename string, rowCount int) (*repository.BulkJob, error)
	// GetJob returns the job (ownership-checked) and a presigned result download URL
	// (empty string if not yet completed).
	GetJob(ctx context.Context, id, ownerID int64) (*repository.BulkJob, string, error)
	ListJobs(ctx context.Context, ownerID int64, limit, offset int) ([]*repository.BulkJob, error)
	DownloadTemplate(format string) (data []byte, contentType, filename string, err error)
}

type bulkJobService struct {
	jobs    repository.BulkJobRepository
	storage storage.R2Client
	baseURL string
}

func NewBulkJobService(jobs repository.BulkJobRepository, s storage.R2Client, baseURL string) BulkJobService {
	return &bulkJobService{jobs: jobs, storage: s, baseURL: baseURL}
}

func (s *bulkJobService) GetUploadURL(ctx context.Context, ownerID int64, filename, contentMD5 string, rowCount int) (string, string, error) {
	ext := filepath.Ext(filename)
	if ext != ".csv" && ext != ".xlsx" {
		return "", "", apperror.UnprocessableEntity("filename must end in .csv or .xlsx")
	}
	if rowCount <= 0 || rowCount > maxBulkRows {
		return "", "", apperror.UnprocessableEntity(fmt.Sprintf("row_count must be between 1 and %d", maxBulkRows))
	}
	if contentMD5 == "" {
		return "", "", apperror.UnprocessableEntity("content_md5 is required")
	}

	fileKey := fmt.Sprintf("bulk/%d/%s%s", ownerID, randomHex(16), ext)
	presignedURL, err := s.storage.PresignedPutURL(ctx, fileKey, contentMD5, presignPutTTL)
	if err != nil {
		return "", "", apperror.Internal(err)
	}
	return presignedURL, fileKey, nil
}

func (s *bulkJobService) ConfirmUpload(ctx context.Context, ownerID int64, fileKey, filename string, rowCount int) (*repository.BulkJob, error) {
	job := &repository.BulkJob{
		OwnerID:   ownerID,
		FileKey:   fileKey,
		Filename:  filename,
		Status:    repository.BulkJobStatusPending,
		TotalRows: rowCount,
	}
	return s.jobs.CreateWithOutbox(ctx, job)
}

func (s *bulkJobService) GetJob(ctx context.Context, id, ownerID int64) (*repository.BulkJob, string, error) {
	job, err := s.jobs.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, "", apperror.NotFound("job not found")
		}
		return nil, "", apperror.Internal(err)
	}
	if job.OwnerID != ownerID {
		return nil, "", apperror.NotFound("job not found")
	}

	var resultURL string
	if job.Status == repository.BulkJobStatusCompleted && job.ResultKey != "" {
		resultURL, err = s.storage.PresignedGetURL(ctx, job.ResultKey, resultGetTTL)
		if err != nil {
			return nil, "", apperror.Internal(err)
		}
	}
	return job, resultURL, nil
}

func (s *bulkJobService) ListJobs(ctx context.Context, ownerID int64, limit, offset int) ([]*repository.BulkJob, error) {
	limit, offset = ClampPaging(limit, offset)
	return s.jobs.ListByOwner(ctx, ownerID, limit, offset)
}

func (s *bulkJobService) DownloadTemplate(format string) ([]byte, string, string, error) {
	if format == "xlsx" {
		f := excelize.NewFile()
		defer f.Close()
		if err := f.SetSheetRow("Sheet1", "A1", &[]interface{}{"url", "result"}); err != nil {
			return nil, "", "", apperror.Internal(err)
		}
		buf, err := f.WriteToBuffer()
		if err != nil {
			return nil, "", "", apperror.Internal(err)
		}
		ct := "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		return buf.Bytes(), ct, "template.xlsx", nil
	}
	// default: csv
	var buf bytes.Buffer
	buf.WriteString("url,result\n")
	return buf.Bytes(), "text/csv", "template.csv", nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
