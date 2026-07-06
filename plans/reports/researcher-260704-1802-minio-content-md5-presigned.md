# MinIO Go SDK v7 Content-MD5 Presigned URL Support Research

**Date:** 2026-07-04  
**Focus:** Content-MD5 enforcement in presigned PUT/POST URLs with minio-go/v7

---

## Executive Summary

The MinIO Go SDK v7 has **limited native Content-MD5 support** for presigned URLs. While the SDK provides multiple presigning methods, none of them offer explicit Content-MD5 constraint parameters. However, there **is a workaround** using the `PresignHeader()` method with custom signed headers.

---

## Method Analysis

### 1. PresignedPutObject()

**Signature:**
```go
func (c *Client) PresignedPutObject(ctx context.Context, bucketName, objectName string, expires time.Duration) (u *url.URL, err error)
```

**Content-MD5 Support:** ❌ NO
- Accepts only: context, bucket, object name, expiration
- No parameters for signed headers or content constraints
- Cannot enforce Content-MD5 on upload

**Limitation:** This is the simplest presign method but unsuitable for Content-MD5 enforcement.

---

### 2. PresignedPostPolicy()

**Signature:**
```go
func (c *Client) PresignedPostPolicy(ctx context.Context, p *PostPolicy) (u *url.URL, formData map[string]string, err error)
```

**Content-MD5 Support:** ❌ NO

**PostPolicy Available Methods:**
- `SetExpires()`, `SetKey()`, `SetBucket()`
- `SetContentType()`, `SetContentTypeStartsWith()`
- `SetContentLengthRange()`, `SetContentDisposition()`, `SetContentEncoding()`
- `SetChecksum()` — supports SHA256, CRC32, CRC32C, SHA1, MD5 (but NOT as a condition constraint)
- `SetTagging()`, `SetUserMetadata()`, `SetSuccessActionRedirect()`, etc.

**Critical Finding:** No `SetContentMD5()` method exists. `SetChecksum()` is for server-side checksums, not for enforcing upload Content-MD5 header validation.

**Known Issue:** GitHub issue [#20319 - "Content-MD5 field/header seems to have no effect with a presigned POST url"](https://github.com/minio/minio/issues/20319) reported that Content-MD5 validation wasn't enforced. Issue marked as **fixed** (#20423), but PostPolicy still lacks explicit Content-MD5 support in the SDK.

---

### 3. PresignHeader() — ✅ WORKAROUND

**Signature:**
```go
func (c *Client) PresignHeader(ctx context.Context, method, bucketName, objectName string, expires time.Duration, reqParams url.Values, extraHeaders http.Header) (u *url.URL, err error)
```

**Content-MD5 Support:** ✅ YES (via custom signed headers)

**How It Works:**
- Accepts `extraHeaders http.Header` parameter
- Headers are included in signature calculation (signed headers)
- Request using presigned URL **must include exact same headers** with same values
- Content-MD5 can be added as a signed header

**Implementation Pattern:**
```go
import (
    "net/http"
    "time"
)

// Create custom headers
headers := http.Header{}
headers.Set("Content-MD5", "your-md5-hash-here")

// Generate presigned URL with signed Content-MD5
url, err := client.PresignHeader(
    ctx,
    http.MethodPut,          // PUT method for upload
    bucketName,
    objectName,
    time.Duration(1000) * time.Second,  // 1000 seconds expiry
    url.Values{},            // no query params needed
    headers,                 // Content-MD5 becomes signed
)
if err != nil {
    // handle error
}

// When uploading, client MUST include:
// Content-MD5: your-md5-hash-here
// Request will fail with 403 SignatureDoesNotMatch if header missing/different
```

**Design Note:** Per PR #1449, `PresignHeader()` was created to support custom signed headers. The function documentation notes: "The extra header parameter should be included in Presign() in the next major version bump, and this function should then be deprecated."

---

## Content-MD5 Enforcement Mechanism

**How AWS S3/MinIO Signature Works:**

When Content-MD5 is included as a signed header:
1. SDK includes `Content-MD5` in the string-to-sign calculation
2. Presigned URL contains signature parameter
3. Client requests with presigned URL must provide identical `Content-MD5` header
4. Server recalculates signature; if headers differ, request fails (403 SignatureDoesNotMatch)
5. Server then **validates actual object MD5 matches** the header value (if enabled)

**Important:** Simply signing the header ensures it's present in the request. The server (MinIO/R2) must separately validate that uploaded content MD5 matches the header.

---

## Cloudflare R2 Compatibility

**Finding:** Cloudflare R2 is S3-compatible and supports presigned URLs. Key points:
- PresignedURLs include signature in URL parameters
- Header validation works via signed headers mechanism
- R2 documentation mentions Content-Type signing restrictions (browser upload issues if Content-Type signed)
- No specific Content-MD5 documentation in R2 presigned URL docs

**Recommendation:** Use `PresignHeader()` with Content-MD5 as a signed header. R2 should respect signature validation like S3.

---

## Known Issues & Gaps

1. **No Native Content-MD5 Support in PresignedPutObject/PresignedPostPolicy**
   - Users must use workaround method (`PresignHeader()`)
   - Inconsistent API (most presign methods lack header signing)

2. **PostPolicy Content-MD5 Enforcement Was Broken**
   - Issue #20319 reported Content-MD5 not enforced in presigned POST (marked fixed)
   - But PostPolicy SDK still lacks explicit setter

3. **Header Signing Limitations**
   - Signing Content-Type in presigned URLs can break browser-based uploads
   - Not all S3-compatible services may respect all signed header constraints

---

## Practical Workaround Summary

**For MinIO Go SDK v7 with Content-MD5 enforcement on presigned PUT URLs:**

```go
// Option A: Use PresignHeader() with custom headers
func GeneratePresignedPutWithMD5(client *minio.Client, bucket, object, md5 string) (*url.URL, error) {
    headers := http.Header{}
    headers.Set("Content-MD5", md5)
    
    return client.PresignHeader(
        context.Background(),
        http.MethodPut,
        bucket,
        object,
        7 * 24 * time.Hour,
        url.Values{},
        headers,
    )
}

// Option B: Custom pre-signature library (if PresignHeader insufficient)
// - Use AWS SDK for Go v2 (aws-sdk-go-v2) which has better header signing
// - Manually sign requests using Signature Version 4 with Content-MD5
```

---

## Unresolved Questions

1. Does Cloudflare R2 enforce Content-MD5 validation in presigned requests, or only validate signature?
2. Are there recent updates (post-v7.0.0) adding native Content-MD5 support to PresignedPutObject?
3. What was the exact fix in PR #20423 for the Content-MD5 PostPolicy issue?

---

## Sources

- [MinIO Go SDK v7 pkg.go.dev](https://pkg.go.dev/github.com/minio/minio-go/v7)
- [minio-go api-presigned.go GitHub](https://github.com/minio/minio-go/blob/master/api-presigned.go)
- [minio-go post-policy.go GitHub](https://github.com/minio/minio-go/blob/master/post-policy.go)
- [PR #1449: Add support for extra signed headers](https://github.com/minio/minio-go/pull/1449)
- [Issue #20319: Content-MD5 field/header seems to have no effect](https://github.com/minio/minio/issues/20319)
- [AWS S3 Presigned URLs Documentation](https://docs.aws.amazon.com/AmazonS3/latest/userguide/using-presigned-url.html)
- [Cloudflare R2 Presigned URLs Docs](https://developers.cloudflare.com/r2/api/s3/presigned-urls/)
