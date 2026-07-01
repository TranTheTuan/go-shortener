package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// FrontendCache adds correct HTTP caching to the embedded SPA. embed.FS reports
// a zero modtime, so Echo's static handler emits neither ETag nor Last-Modified
// and every request re-downloads the full body. This middleware precomputes a
// content-hash ETag per asset once at startup and answers conditional requests
// with 304 Not Modified, so unchanged files skip the body while a new deploy is
// still picked up immediately — Cache-Control: no-cache means "revalidate
// before use", not "don't store".
//
// It is registered globally but only acts on paths it has an ETag for (the
// embedded frontend); every other request falls straight through to next.
func FrontendCache(files fs.FS) echo.MiddlewareFunc {
	etags := buildETags(files)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			etag, ok := etags[c.Request().URL.Path]
			if !ok {
				return next(c)
			}
			h := c.Response().Header()
			h.Set("Cache-Control", "no-cache")
			h.Set("ETag", etag)
			if etagMatches(c.Request().Header.Get("If-None-Match"), etag) {
				return c.NoContent(http.StatusNotModified)
			}
			return next(c)
		}
	}
}

// buildETags walks the embedded filesystem and maps every served URL path to a
// strong content-hash ETag. index.html is also mapped to "/" because that is
// where FileFS serves it.
func buildETags(files fs.FS) map[string]string {
	etags := make(map[string]string)
	_ = fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, readErr := fs.ReadFile(files, p)
		if readErr != nil {
			return nil // skip unreadable entries rather than fail startup
		}
		sum := sha256.Sum256(data)
		tag := `"` + hex.EncodeToString(sum[:]) + `"`
		etags["/"+p] = tag
		if p == "index.html" {
			etags["/"] = tag
		}
		return nil
	})
	return etags
}

// etagMatches reports whether an If-None-Match header value matches etag,
// tolerating a comma-separated list, weak-validator prefixes, and "*".
func etagMatches(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	for part := range strings.SplitSeq(header, ",") {
		if strings.TrimPrefix(strings.TrimSpace(part), "W/") == etag {
			return true
		}
	}
	return false
}
