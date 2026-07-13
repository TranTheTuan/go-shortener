package worker

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
)

// --- minimal mocks ---

type mockBulkRepo struct {
	job         *repository.BulkJob
	statusCalls []string
	resultKey   string
	doneRows    int
}

func (m *mockBulkRepo) CreateWithOutbox(_ context.Context, job *repository.BulkJob) (*repository.BulkJob, error) {
	return job, nil
}
func (m *mockBulkRepo) GetByID(_ context.Context, _ int64) (*repository.BulkJob, error) {
	return m.job, nil
}
func (m *mockBulkRepo) ListByOwner(_ context.Context, _ int64, _, _ int) ([]*repository.BulkJob, error) {
	return nil, nil
}
func (m *mockBulkRepo) UpdateStatus(_ context.Context, _ int64, status string, _ int) error {
	m.statusCalls = append(m.statusCalls, status)
	return nil
}
func (m *mockBulkRepo) UpdateResult(_ context.Context, _ int64, resultKey string, doneRows int) error {
	m.resultKey = resultKey
	m.doneRows = doneRows
	return nil
}
func (m *mockBulkRepo) RelayOutbox(_ context.Context, fn func(int64) error) error {
	return nil
}

type mockLinkService struct {
	// shortURL returned on success; empty = return bad-request error
	shortURL string
}

func (m *mockLinkService) Create(_ context.Context, in service.CreateLinkInput) (*repository.Link, bool, error) {
	if m.shortURL == "" {
		return nil, false, apperror.BadRequest("invalid url")
	}
	return &repository.Link{ShortCode: strings.TrimPrefix(m.shortURL, "http://short/")}, false, nil
}
func (m *mockLinkService) BatchCreate(_ context.Context, _ int64, urls []string) ([]*repository.Link, []error) {
	links := make([]*repository.Link, len(urls))
	errs := make([]error, len(urls))
	for i, u := range urls {
		if u == "" || m.shortURL == "" {
			errs[i] = apperror.BadRequest("invalid url")
		} else {
			links[i] = &repository.Link{ID: int64(i + 1), ShortCode: strings.TrimPrefix(m.shortURL, "http://short/")}
		}
	}
	return links, errs
}
func (m *mockLinkService) Resolve(_ context.Context, _ string) (*repository.Link, error) {
	return nil, nil
}
func (m *mockLinkService) ListByOwner(_ context.Context, _ int64, _ string, _, _ int) ([]*repository.Link, int64, error) {
	return nil, 0, nil
}
func (m *mockLinkService) Delete(_ context.Context, _ string, _ int64) (*repository.Link, error) {
	return nil, nil
}
func (m *mockLinkService) Update(_ context.Context, _ string, _ int64, _ *time.Time, _ bool) (*repository.Link, error) {
	return nil, nil
}

type mockStorage struct {
	// content is returned by Download; Upload captures what was written
	content   string
	uploaded  []byte
	uploadKey string
}

func (m *mockStorage) PresignedPutURL(_ context.Context, _, _ string, _ time.Duration) (string, error) {
	return "https://r2/presign", nil
}
func (m *mockStorage) PresignedGetURL(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "https://r2/presign-get", nil
}
func (m *mockStorage) Download(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(m.content)), nil
}
func (m *mockStorage) Upload(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	m.uploadKey = key
	data, _ := io.ReadAll(r)
	m.uploaded = data
	return nil
}

// --- tests ---

func TestProcess_SkipsNonPending(t *testing.T) {
	repo := &mockBulkRepo{job: &repository.BulkJob{ID: 1, Status: repository.BulkJobStatusCompleted}}
	w := NewBulkJobWorker(repo, &mockLinkService{shortURL: "http://short/abc"}, &mockStorage{}, "http://short")
	if err := w.Process(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if len(repo.statusCalls) > 0 {
		t.Fatal("expected no status updates for non-pending job")
	}
}

func TestProcess_CSVRoundTrip(t *testing.T) {
	csvContent := "url,result\nhttps://example.com,\nhttps://go.dev,\n"
	stor := &mockStorage{content: csvContent}
	repo := &mockBulkRepo{job: &repository.BulkJob{
		ID: 2, OwnerID: 1, FileKey: "uploads/file.csv",
		Status: repository.BulkJobStatusPending, TotalRows: 2,
	}}
	links := &mockLinkService{shortURL: "http://short/abc"}
	w := NewBulkJobWorker(repo, links, stor, "http://short")

	if err := w.Process(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	if repo.resultKey != "uploads/file-result.csv" {
		t.Errorf("resultKey = %q", repo.resultKey)
	}
	if repo.doneRows != 2 {
		t.Errorf("doneRows = %d, want 2", repo.doneRows)
	}
	// Result CSV must contain the short URL in the second column.
	rows, _ := Read(bytes.NewReader(stor.uploaded), ".csv")
	if len(rows) < 2 || rows[1][1] != "http://short/abc" {
		t.Errorf("unexpected result rows: %v", rows)
	}
}

func TestProcess_InvalidURL(t *testing.T) {
	csvContent := "url,result\nnot-a-url,\n"
	stor := &mockStorage{content: csvContent}
	repo := &mockBulkRepo{job: &repository.BulkJob{
		ID: 3, OwnerID: 1, FileKey: "uploads/file.csv",
		Status: repository.BulkJobStatusPending, TotalRows: 1,
	}}
	links := &mockLinkService{} // returns bad-request error
	w := NewBulkJobWorker(repo, links, stor, "http://short")

	if err := w.Process(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	rows, _ := Read(bytes.NewReader(stor.uploaded), ".csv")
	if rows[1][1] != "url không hợp lệ" {
		t.Errorf("expected invalid-url message, got %q", rows[1][1])
	}
}
