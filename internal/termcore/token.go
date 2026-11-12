package termcore

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// NewToken mints an opaque base64url identifier of n random bytes whose
// first character is a letter or digit. base64url's alphabet also holds '-'
// and '_', and an id that starts with either breaks where people and other
// systems take it: a command line reads a leading '-' as a flag (`observer
// terminals stop -XJMm...` failed with "unknown shorthand flag", live check
// 2026-11-12), and a hosted box refuses a label value that does not start
// with a letter or digit. A value that starts with one is drawn again; the
// chance is 1 in 32 per draw, so eight draws all failing is about 1 in 10^12
// and reported as an error rather than returned.
func NewToken(n int) (string, error) {
	if n <= 0 {
		return "", fmt.Errorf("termcore: token of %d bytes", n)
	}
	b := make([]byte, n)
	for i := 0; i < 8; i++ {
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("termcore: read random bytes: %w", err)
		}
		id := base64.RawURLEncoding.EncodeToString(b)
		if TokenStartOK(id) {
			return id, nil
		}
	}
	return "", fmt.Errorf("termcore: could not mint a token")
}

// TokenStartOK reports whether id starts with a letter or digit.
func TokenStartOK(id string) bool {
	if id == "" {
		return false
	}
	c := id[0]
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
