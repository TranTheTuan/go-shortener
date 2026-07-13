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

// DownloadTemplate handles GET /api/bulk-jobs/template.
//
// @Summary      Download the bulk-upload template
// @Description  Returns an empty template (columns: url, result) for the user to fill in and upload.
// @Tags         bulk-jobs
// @Produce      text/csv
// @Produce      application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Security     BearerAuth
// @Param        format  query     string  false  "Template format"  Enums(csv, xlsx)  default(csv)
// @Success      200     {file}    binary             "the template file"
// @Failure      401     {object}  response.Envelope  "missing or invalid token"
// @Router       /api/bulk-jobs/template [get]
func (h *BulkJobHandler) DownloadTemplate(c echo.Context) error {
	format := c.QueryParam("format")
	data, ct, filename, err := h.svc.DownloadTemplate(format)
	if err != nil {
		return response.Error(c, err)
	}
	c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="`+filename+`"`)
	return c.Blob(http.StatusOK, ct, data)
}

// GetUploadURL handles POST /api/bulk-jobs/upload-url.
//
// @Summary      Get a presigned URL to upload a bulk file
// @Description  Returns a presigned R2 URL the client PUTs the filled CSV/XLSX to, plus the file_key used to confirm the upload.
// @Tags         bulk-jobs
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      uploadURLRequest   true  "File metadata (filename, row_count, content_md5)"
// @Success      200      {object}  uploadURLResponse
// @Failure      400      {object}  response.Envelope  "invalid request body"
// @Failure      401      {object}  response.Envelope  "missing or invalid token"
// @Router       /api/bulk-jobs/upload-url [post]
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

// ConfirmUpload handles POST /api/bulk-jobs.
//
// @Summary      Confirm an upload and start a bulk job
// @Description  Registers a previously uploaded file (by file_key) as a bulk job and queues it for processing.
// @Tags         bulk-jobs
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      confirmRequest     true  "Uploaded file key + metadata"
// @Success      201      {object}  bulkJobResponse
// @Failure      400      {object}  response.Envelope  "invalid request body"
// @Failure      401      {object}  response.Envelope  "missing or invalid token"
// @Router       /api/bulk-jobs [post]
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

// ListJobs handles GET /api/bulk-jobs.
//
// @Summary      List the caller's bulk jobs
// @Tags         bulk-jobs
// @Produce      json
// @Security     BearerAuth
// @Param        limit   query     int  false  "Page size"
// @Param        offset  query     int  false  "Offset into the result set"
// @Success      200     {array}   bulkJobResponse
// @Failure      401     {object}  response.Envelope  "missing or invalid token"
// @Router       /api/bulk-jobs [get]
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

// GetResultURL handles GET /api/bulk-jobs/:id/download-url.
//
// @Summary      Get a presigned download URL for a completed bulk job
// @Description  Returns a short-lived presigned URL to download the result file. Returns 409 if job is not yet completed.
// @Tags         bulk-jobs
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      int  true  "Bulk job ID"
// @Success      200  {object}  map[string]string
// @Failure      401  {object}  response.Envelope  "missing or invalid token"
// @Failure      404  {object}  response.Envelope  "job not found"
// @Failure      409  {object}  response.Envelope  "result not available yet"
// @Router       /api/bulk-jobs/{id}/download-url [get]
func (h *BulkJobHandler) GetResultURL(c echo.Context) error {
	owner, ok := appmw.UserIDFrom(c)
	if !ok {
		return response.Error(c, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated"))
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return response.Error(c, apperror.NotFound("job not found"))
	}
	_, url, err := h.svc.GetJob(c.Request().Context(), id, owner)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]string{"url": url})
}

// @Summary      Get a bulk job by ID
// @Description  Returns the job's status/progress and, once finished, a result_url to download the processed file.
// @Tags         bulk-jobs
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      int  true  "Bulk job ID"
// @Success      200  {object}  bulkJobResponse
// @Failure      401  {object}  response.Envelope  "missing or invalid token"
// @Failure      404  {object}  response.Envelope  "job not found"
// @Router       /api/bulk-jobs/{id} [get]
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
