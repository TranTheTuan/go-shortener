# Postmortem: Frontend cũ vẫn hiển thị sau khi deploy (stale cache)

- **Ngày:** 2026-07-01
- **Phạm vi ảnh hưởng:** `go-short.tonytran.xyz` (frontend SPA nhúng trong binary Go)
- **Trạng thái:** Đã khắc phục
- **Repo chứa fix:** `go-shortener` · **Repo hạ tầng liên quan:** `go-shortener-infra`

---

## 1. Triệu chứng

Có hai vấn đề xuất hiện nối tiếp nhau khi truy cập domain trên Cloudflare:

1. Trình duyệt báo `DNS_PROBE_FINISHED_NXDOMAIN` khi vào `go-short.tonytran.xyz`.
2. Sau khi khắc phục DNS: mỗi lần cập nhật frontend (thư mục `web/`) và deploy qua ArgoCD, **tải lại trang vẫn thấy code frontend cũ**.

---

## 2. Nguyên nhân gốc

### 2.1. Lỗi DNS (`NXDOMAIN`)

Bản ghi `go-short.tonytran.xyz` là một CNAME trỏ tới Cloudflare Tunnel:

```
go-short.tonytran.xyz  →  CNAME  →  <id>.cfargotunnel.com
```

Domain `*.cfargotunnel.com` **không có A record công khai** — nó chỉ phân giải ra IP khi bản ghi được **Proxied (mây cam)** qua Cloudflare. Bản ghi đang ở chế độ **DNS only (mây xám)**, nên chuỗi CNAME dừng ở một tên không có IP → trình duyệt báo `NXDOMAIN`.

> **Quy tắc:** mọi CNAME trỏ tới `*.cfargotunnel.com` bắt buộc phải Proxied.

### 2.2. Frontend cũ (stale cache) — nguyên nhân chính

Frontend được serve bằng `embed.FS` với **tên file cố định, không có content-hash**:

- `web/index.html` → `/`
- `/static/app.js`, `/static/styles.css`, `/static/keycloak.js`

Hai yếu tố kết hợp gây ra stale cache:

1. **Code không set header cache nào** (`Cache-Control`, `ETag`, `Last-Modified`).
2. **`embed.FS` báo modtime = zero**, nên `http.ServeContent` của Go không sinh `Last-Modified` và không tự tạo `ETag` → **không có validator nào để revalidate**.

Khi bật Cloudflare Proxied, edge **mặc định cache file tĩnh theo đuôi** (`.js`, `.css`). Sau khi deploy, URL asset **vẫn y hệt** (`/static/app.js`) và không có validator, nên Cloudflare (và cả trình duyệt) tiếp tục trả bản cũ cho tới khi hết TTL. HTML thì không bị CF cache mặc định → dẫn tới tình trạng HTML mới nhưng JS/CSS cũ.

---

## 3. Các giải pháp được cân nhắc

### Sửa DNS
Chuyển bản ghi `go-short` trong Cloudflare từ **DNS only** sang **Proxied**. (Đã xử lý ở tầng cấu hình Cloudflare, không thuộc code.)

### Sửa stale cache — các phương án serve frontend

| # | Phương án | Ưu | Nhược |
|---|-----------|-----|-------|
| A | **Giữ embed + thêm `ETag`/`Cache-Control` middleware** | 1 binary, deploy đơn giản, ít công sức nhất | Không tận dụng edge cache mạnh (xem mục 5) |
| B | **nginx sidecar** serve static, Go chỉ serve API/config | ETag/gzip/brotli native, single origin, hỗ trợ asset hash + `immutable` | Thêm container + build image frontend |
| C | **Cloudflare Pages** cho frontend | CDN toàn cầu, deploy nguyên tử, asset hash sẵn | Phá vỡ same-origin → cần CORS/proxy; `app-config.json` phải thành build artifact/Function |
| D | Content-hash tên asset + `immutable` (áp cho B hoặc embed) | Cache tối đa + luôn mới, không cần purge | Cần build tool cho frontend |

**Về ý nghĩa của `no-cache` (điểm mấu chốt khi đánh giá phương án A):**

- `no-store` = cấm lưu hoàn toàn → mất cache.
- `no-cache` = **vẫn được lưu, nhưng phải revalidate trước khi dùng**. Nếu có validator (`ETag`), file không đổi sẽ trả `304 Not Modified` (không body) → **vẫn tiết kiệm băng thông**.

---

## 4. Lý do chọn phương án cuối cùng (A)

Frontend hiện tại là **vanilla JS nhỏ, gắn chặt same-origin** (fetch `/app-config.json`, `/api/*`, `/auth/me` cùng origin, auth bằng Bearer token của Keycloak). Với đặc điểm đó:

- **Phương án C (Pages)** bị loại vì phá vỡ same-origin, kéo theo CORS/proxy và tách `app-config.json` — quá nặng so với quy mô app.
- **Phương án B (nginx)** chuẩn hơn nhưng thêm container + pipeline build frontend — chưa cần thiết cho app nhỏ.
- **Phương án A** chỉ cần ~1 file middleware, giữ nguyên kiến trúc 1 binary + same-origin, và khắc phục đúng 2 điểm yếu duy nhất của embed (thiếu validator, thiếu cache header).

→ Chọn **A**: tỷ lệ lợi ích/công sức cao nhất, không phát sinh CORS hay hạ tầng mới.

---

## 5. Giải pháp cuối cùng — chi tiết

Thêm middleware tính `ETag` từ hash nội dung file embed và trả `304` cho request có điều kiện. `no-cache` buộc revalidate nên deploy mới được nhận ngay, còn `ETag` giúp file không đổi trả `304` (không tải lại body).

**Các thay đổi:**

| File | Nội dung |
|------|----------|
| `internal/middleware/frontend_cache.go` | Middleware `FrontendCache`: duyệt `embed.FS`, tính `ETag = sha256(nội dung)` cho từng file **1 lần lúc startup**; set `Cache-Control: no-cache` + `ETag`; trả `304 Not Modified` khi `If-None-Match` khớp. Đăng ký global nhưng chỉ tác động lên path của SPA. |
| `internal/router/router.go` | Đăng ký `appmw.FrontendCache(web.Files)` sau `Recover()`. |
| `internal/handler/frontend_handler.go` | `/app-config.json` set `Cache-Control: no-cache` để config auth không bị stale. |
| `internal/middleware/frontend_cache_test.go` | Test: set header đúng, `304` khi ETag khớp, serve full khi ETag cũ, passthrough với path lạ. |

**Hành vi sau khi sửa:**

- Mỗi asset có `ETag` thật → trình duyệt/Cloudflare revalidate và nhận `304` (không tải body) khi file không đổi.
- Sau deploy, nội dung đổi → hash đổi → `ETag` mới → client tự nhận bản mới, **không cần purge thủ công**.
- `no-cache` giữ được lợi ích băng thông qua `304`, đồng thời tránh lệch phiên bản HTML/JS (do tên file cố định, không hash).

---

## 6. Hạn chế đã biết & hướng cải tiến

- Mặc định Cloudflare coi `no-cache` là **không serve từ edge cache** → mỗi request vẫn về origin (nhưng origin trả `304` nhẹ). Chưa tận dụng tối đa edge cache.
- **Hướng nâng cấp (phương án D):** đặt content-hash vào tên asset (`app.<hash>.js`) + `Cache-Control: public, max-age=31536000, immutable` cho `/static/*`, giữ `no-cache` cho `index.html`. Khi đó asset cache vĩnh viễn ở edge, deploy mới = URL mới = tự bust cache. Cần thêm build tool cho frontend.

---

## 7. Bài học

- CNAME trỏ tới `*.cfargotunnel.com` **phải** được Proxied trên Cloudflare.
- `embed.FS` không có modtime → **luôn cần tự set `ETag`/`Cache-Control`** nếu muốn caching hoạt động đúng.
- Với asset **tên cố định**, `no-cache` + `ETag` là lựa chọn an toàn (tránh lệch phiên bản). Với asset **có content-hash**, dùng `immutable` để cache tối đa.
