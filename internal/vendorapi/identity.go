package vendorapi

import "strings"

// FoldEmail folds a vendor e-mail for landing (security ledger VCONN-6,
// T-701): surrounding space is trimmed and ASCII letters are lower-cased;
// every other byte is kept as it is. A Unicode case fold (strings.ToLower)
// maps the Kelvin sign U+212A onto "k" and so could turn a look-alike
// address into a member's address before the matching seam sees it; with an
// ASCII-only fold such an address keeps its non-ASCII byte, and the seam
// (vendorexportstore.ActiveMemberByEmail, ASCII only on both sides) holds it
// for an explicit admin link instead of matching it.
func FoldEmail(s string) string {
	s = strings.TrimSpace(s)
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
