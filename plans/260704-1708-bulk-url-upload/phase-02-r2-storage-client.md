# Phase 02 — R2 Storage Client

## Context Links
- Design spec: `../reports/design-260704-1708-bulk-url-upload.md` (Components → pkg/storage/r2_client.go)
- Pattern refs: `pkg/database/` (constructor + options style), `configs.R2Config` (phase 01)

## Overview
- **Priority:** P1
- **Status:** pending
- **Description:** Thin MinIO-SDK-v7 wrapper for R2: presigned PUT (Content-MD5 baked into signature), presigned GET, download, upload (with server-side MD5). Interface so worker/service mock it.

## Key Insights
- R2 is S3-compatible; MinIO v7 works with it. Use `Secure: true`, `Region: "auto"`, endpoint host only (no scheme).
- **Presigned PUT with Content-MD5:** `PresignHeader(ctx, PUT, bucket, key, ttl, url.Values{}, headers)` where `headers` has `Content-MD5`. R2 returns `403 SignatureDoesNotMatch` if the client omits/changes the header → integrity enforced at edge.
- **Worker upload:** `PutObject(..., minio.PutObjectOptions{SendContentMd5: true, ContentType: ...})` → SDK computes+sends MD5.
- Keep interface small (YAGNI): only the 4 methods callers need.

## Requirements
- **Functional:** Generate presigned URLs, stream download, stream upload.
- **Non-functional:** context-aware (timeouts); no global state; constructor returns `error` if client init fails.

## Architecture / Design
```go
package storage

type R2Client interface {
    PresignedPutURL(ctx context.Context, key, contentMD5 string, ttl time.Duration) (string, error)
    PresignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error)
    Download(ctx context.Context, key string) (io.ReadCloser, error)
    Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
}

type r2Client struct {
    mc     *minio.Client
    bucket string
}

func NewR2Client(cfg configs.R2Config) (R2Client, error)
```

## Related Code Files
- **Create:** `pkg/storage/r2_client.go`
- **Modify:** `go.mod`/`go.sum` (via `go get github.com/minio/minio-go/v7`)

## Implementation Steps
1. `go get github.com/minio/minio-go/v7@latest`.
2. `NewR2Client`:
   ```go
   mc, err := minio.New(cfg.Endpoint(), &minio.Options{
       Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
       Secure: true,
       Region: "auto",
   })
   if err != nil { return nil, fmt.Errorf("r2: new client: %w", err) }
   return &r2Client{mc: mc, bucket: cfg.Bucket}, nil
   ```
3. `PresignedPutURL`:
   ```go
   h := http.Header{}
   h.Set("Content-MD5", contentMD5)
   u, err := c.mc.PresignHeader(ctx, http.MethodPut, c.bucket, key, ttl, url.Values{}, h)
   if err != nil { return "", err }
   return u.String(), nil
   ```
4. `PresignedGetURL`: `c.mc.PresignedGetObject(ctx, c.bucket, key, ttl, url.Values{})`.
5. `Download`: `c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})` returns `*minio.Object` (implements `io.ReadCloser`).
6. `Upload`:
   ```go
   _, err := c.mc.PutObject(ctx, c.bucket, key, r, size,
       minio.PutObjectOptions{ContentType: contentType, SendContentMd5: true})
   return err
   ```
   Note: pass `size=-1` when unknown (SDK multipart-streams); worker knows buffer len so pass exact size.
7. `go build ./...`.

## Todo List
- [ ] `go get minio-go/v7`
- [ ] Implement `NewR2Client`
- [ ] `PresignedPutURL` with Content-MD5 header
- [ ] `PresignedGetURL`
- [ ] `Download` / `Upload`
- [ ] `go build ./...` passes

## Success Criteria
- Package compiles; interface satisfied by `r2Client`.
- Manual smoke (optional): presign a PUT, upload a file with matching Content-MD5 → 200; mismatched → 403.

## Risk Assessment
- **Endpoint format:** MinIO wants host without scheme; `Secure:true` adds https. Verify `Endpoint()` returns no `https://` prefix.
- **Clock skew** on presign TTL — 15m PUT window is generous.
- **R2 quirks:** `Region:"auto"` required; some SDK calls need it. If presign fails, try explicit region.

## Unresolved Questions
- R2 bucket auto-created or must pre-exist? (Assume pre-provisioned via infra; no `MakeBucket` call — YAGNI.)
