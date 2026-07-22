package useragent_test

import (
	"testing"

	"github.com/TranTheTuan/go-shortener/pkg/useragent"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		ua      string
		device  string
		browser string
		os      string
	}{
		{"empty", "", "unknown", "unknown", "unknown"},
		{"desktop chrome", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36", "desktop", "Chrome", "Windows"},
		{"mobile safari", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", "mobile", "Safari", "iOS"},
		{"googlebot", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", "bot", "Googlebot", ""},
		{"garbage", "not-a-ua-string!!!!", "unknown", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := useragent.Parse(tc.ua)
			if r.Device != tc.device {
				t.Errorf("device: got %q want %q", r.Device, tc.device)
			}
			if r.Browser != tc.browser && tc.browser != "" {
				t.Errorf("browser: got %q want %q", r.Browser, tc.browser)
			}
			if r.OS != tc.os && tc.os != "" {
				t.Errorf("os: got %q want %q", r.OS, tc.os)
			}
			if len(r.Browser) > 40 {
				t.Errorf("browser exceeds 40 chars: %q", r.Browser)
			}
			if len(r.OS) > 40 {
				t.Errorf("os exceeds 40 chars: %q", r.OS)
			}
		})
	}
}

func TestParseGooglebotFallback(t *testing.T) {
	r := useragent.Parse("Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	if r.Browser == "" {
		t.Error("browser should not be empty string (fallback to unknown)")
	}
}
