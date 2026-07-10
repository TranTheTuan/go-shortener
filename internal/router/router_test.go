package router

import "testing"

func TestSkipTrace(t *testing.T) {
	cases := map[string]bool{
		"/:code":            true,  // redirect hot path — excluded to protect L1 perf
		"/healthz":          true,  // infra noise
		"/metrics":          true,  // infra noise
		"/api/links":        false, // traced
		"/api/links/:code":  false, // traced
		"/auth/me":          false, // traced
		"/":                 false, // traced
	}
	for path, want := range cases {
		if got := skipTrace(path); got != want {
			t.Errorf("skipTrace(%q) = %v, want %v", path, got, want)
		}
	}
}
