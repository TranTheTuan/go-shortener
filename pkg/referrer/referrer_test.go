package referrer_test

import (
	"strings"
	"testing"

	"github.com/TranTheTuan/go-shortener/pkg/referrer"
)

func TestDomain(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", "direct"},
		{"full url", "https://www.google.com/search?q=test", "google.com"},
		{"no www", "https://github.com/foo", "github.com"},
		{"www stripped", "https://www.example.com", "example.com"},
		{"scheme-less", "example.com/path", "direct"},
		{"just path", "/some/path", "direct"},
		{"garbage", "not a url !!", "direct"},
		{"uppercase host", "https://GOOGLE.COM/", "google.com"},
		{"overlength domain", "https://" + strings.Repeat("a", 260) + ".com/", strings.Repeat("a", 255)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := referrer.Domain(tc.input)
			if got != tc.want {
				t.Errorf("Domain(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}
