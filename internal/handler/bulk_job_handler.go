package handler

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	appmw "github.com/TranTheTuan/go-shortener/internal/middleware"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/response"
)

// BulkJobHandler exposes the bulk URL upload endpoints.
type BulkJobHandler struct {
	svc service.BulkJobService
}

func NewBulkJobHandler(svc service.BulkJobService) *BulkJobHandler {
	return &BulkJobHandler{svc: svc}
}

type uploadURLRequest struct {
	Filename   string `json:"filename"`
	RowCount   int    `json:"row_count"`
	ContentMD5 string `json:"content_md5"`
}

type uploadURLResponse struct {
	PresignedURL string `json:"presigned_url"`
	FileKey      string `json:"file_key"`
}

type confirmRequest struct {
	FileKey  string `json:"file_key"`
	Filename string `json:"filename"`
	RowCount int    `json:"row_count"`
}

type bulkJobResponse struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	TotalRows int    `json:"total_rows"`
	DoneRows  int    `json:"done_rows"`
	ResultURL string `json:"result_url,omitempty"`
	CreatedAt string `json:"created_at"`
}

// DownloadTemplate handles GET /api/bulk-jobs/template?format=csv|xlsx
func (h *BulkJobHandler) DownloadTemplate(c echo.Context) error {
	format := c.QueryParam("format")
	data, ct, filename, err := h.svc.DownloadTemplate(format)
	if err != nil {
		return response.Error(c, err)
	}
	c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="`+filename+`"`)
	return c.Blob(http.StatusOK, ct, data)
}

// GetUploadURL handles POST /api/bulk-jobs/upload-url
func (h *BulkJobHandler) GetUploadURL(c echo.Context) error {
	owner, ok := appmw.UserIDFrom(c)
	if !ok {
		return response.Error(c, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated"))
	}
	var req uploadURLRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}
	presignedURL, fileKey, err := h.svc.GetUploadURL(c.Request().Context(), owner, req.Filename, req.ContentMD5, req.RowCount)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, http.StatusOK, uploadURLResponse{PresignedURL: presignedURL, FileKey: fileKey})
}

// ConfirmUpload handles POST /api/bulk-jobs
func (h *BulkJobHandler) ConfirmUpload(c echo.Context) error {
	owner, ok := appmw.UserIDFrom(c)
	if !ok {
		return response.Error(c, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated"))
	}
	var req confirmRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}
	job, err := h.svc.ConfirmUpload(c.Request().Context(), owner, req.FileKey, req.Filename, req.RowCount)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, http.StatusCreated, bulkJobResponse{
		ID:        job.ID,
		Status:    job.Status,
		TotalRows: job.TotalRows,
		CreatedAt: job.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// ListJobs handles GET /api/bulk-jobs
func (h *BulkJobHandler) ListJobs(c echo.Context) error {
	owner, ok := appmw.UserIDFrom(c)
	if !ok {
		return response.Error(c, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated"))
	}
	limit := atoiDefault(c.QueryParam("limit"), 0)
	offset := atoiDefault(c.QueryParam("offset"), 0)
	jobs, err := h.svc.ListJobs(c.Request().Context(), owner, limit, offset)
	if err != nil {
		return response.Error(c, apperror.Internal(err))
	}
	out := make([]bulkJobResponse, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, bulkJobResponse{
			ID:        j.ID,
			Status:    j.Status,
			TotalRows: j.TotalRows,
			DoneRows:  j.DoneRows,
			CreatedAt: j.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return response.Success(c, http.StatusOK, out)
}

// GetJob handles GET /api/bulk-jobs/:id
func (h *BulkJobHandler) GetJob(c echo.Context) error {
	owner, ok := appmw.UserIDFrom(c)
	if !ok {
		return response.Error(c, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated"))
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return response.Error(c, apperror.NotFound("job not found"))
	}
	job, resultURL, err := h.svc.GetJob(c.Request().Context(), id, owner)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, http.StatusOK, bulkJobResponse{
		ID:        job.ID,
		Status:    job.Status,
		TotalRows: job.TotalRows,
		DoneRows:  job.DoneRows,
		ResultURL: resultURL,
		CreatedAt: job.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}
