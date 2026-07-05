// Package storage provides a thin Cloudflare R2 client backed by the MinIO SDK.
// R2 is S3-compatible; MinIO v7 connects via the account-specific endpoint with
// static credentials and AWS Signature V4.
package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/TranTheTuan/go-shortener/configs"
)

// R2Client is the interface callers use — keeps service/worker testable via mocks.
type R2Client interface {
	// PresignedPutURL generates a presigned PUT URL with Content-MD5 baked into
	// the signature. The uploader must send the matching Content-MD5 header or R2
	// returns 403 SignatureDoesNotMatch — enforcing file integrity at the edge.
	PresignedPutURL(ctx context.Context, key, contentMD5 string, ttl time.Duration) (string, error)
	PresignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error)
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
}

type r2Client struct {
	mc     *minio.Client
	bucket string
}

// NewR2Client constructs an R2Client from config.
// Returns an error if the MinIO client cannot be initialised.
func NewR2Client(cfg configs.R2Config) (R2Client, error) {
	mc, err := minio.New(fmt.Sprintf("%s.r2.cloudflarestorage.com", cfg.AccountID), &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: true,
		Region: "auto",
	})
	if err != nil {
		return nil, fmt.Errorf("r2: new client: %w", err)
	}
	return &r2Client{mc: mc, bucket: cfg.Bucket}, nil
}

func (c *r2Client) PresignedPutURL(ctx context.Context, key, contentMD5 string, ttl time.Duration) (string, error) {
	h := http.Header{}
	h.Set("Content-MD5", contentMD5)
	u, err := c.mc.PresignHeader(ctx, http.MethodPut, c.bucket, key, ttl, url.Values{}, h)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (c *r2Client) PresignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := c.mc.PresignedGetObject(ctx, c.bucket, key, ttl, url.Values{})
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (c *r2Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	return c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
}

// Upload streams r to R2 with server-side MD5 verification (SendContentMd5: true).
// Pass size=-1 if unknown (SDK will multipart-stream).
func (c *r2Client) Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := c.mc.PutObject(ctx, c.bucket, key, r, size,
		minio.PutObjectOptions{ContentType: contentType, SendContentMd5: true})
	return err
}
