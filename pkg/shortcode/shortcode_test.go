package shortcode

import (
	"strings"
	"testing"
)

func TestGenerate_LengthAndAlphabet(t *testing.T) {
	for _, n := range []int{1, 5, 7, 12, 32} {
		code, err := Generate(n)
		if err != nil {
			t.Fatalf("Generate(%d) returned error: %v", n, err)
		}
		if len(code) != n {
			t.Errorf("Generate(%d) length = %d, want %d", n, len(code), n)
		}
		for _, r := range code {
			if !strings.ContainsRune(alphabet, r) {
				t.Errorf("Generate(%d) produced char %q outside base62 alphabet", n, r)
			}
		}
	}
}

func TestGenerate_NonPositiveDefaultsToSeven(t *testing.T) {
	for _, n := range []int{0, -3} {
		code, err := Generate(n)
		if err != nil {
			t.Fatalf("Generate(%d) returned error: %v", n, err)
		}
		if len(code) != 7 {
			t.Errorf("Generate(%d) length = %d, want default 7", n, len(code))
		}
	}
}

func TestGenerate_ProducesVaryingCodes(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		code, err := Generate(7)
		if err != nil {
			t.Fatalf("Generate returned error: %v", err)
		}
		seen[code] = struct{}{}
	}
	// With 62^7 space, 1000 draws colliding more than a handful is implausible.
	if len(seen) < 995 {
		t.Errorf("got %d unique codes out of 1000, expected near-unique output", len(seen))
	}
}
