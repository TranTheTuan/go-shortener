// Package shortcode generates random, URL-safe short codes for links. Codes are
// drawn from a base62 alphabet using crypto/rand so they are hard to guess and
// enumerate.
package shortcode

import "crypto/rand"

// alphabet is the base62 character set: digits + upper + lower case letters.
const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// Generate returns a cryptographically-random base62 string of length n.
//
// Each byte from crypto/rand is mapped onto the 62-char alphabet via modulo.
// This introduces a tiny bias toward the first (256 mod 62 = 8) characters,
// which is negligible for short-link collision purposes (YAGNI on rejection
// sampling).
func Generate(n int) (string, error) {
	if n <= 0 {
		n = 7
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}
