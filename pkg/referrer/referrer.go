package referrer

import (
	"net/url"
	"strings"
)

// Domain returns the lowercased hostname of ref with www. stripped (≤255 chars).
// Empty, unparseable, or host-less referrers return "direct".
func Domain(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "direct"
	}
	u, err := url.Parse(ref)
	if err != nil || u.Host == "" {
		return "direct"
	}
	h := strings.ToLower(u.Hostname())
	h = strings.TrimPrefix(h, "www.")
	if len(h) > 255 {
		h = h[:255]
	}
	if h == "" {
		return "direct"
	}
	return h
}
