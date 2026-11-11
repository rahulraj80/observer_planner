package sandhold

import (
	"io"
	"strings"
	"unicode/utf8"
)

// Plain removes control characters (C0 but tab and newline, DEL, C1) from
// a string a node sent, so nothing in a reason, a name or a hint can drive
// the person's terminal (note 42 section 5, the last finding).
func Plain(s string) string {
	// An invalid byte (a raw 0x9b is an 8-bit CSI) is replaced, never kept.
	if utf8.ValidString(s) && strings.IndexFunc(s, isControl) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !isControl(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// PlainLine is Plain that also folds newlines and tabs into spaces, for a
// node string printed inside one line.
func PlainLine(s string) string {
	return strings.Join(strings.Fields(Plain(s)), " ")
}

func isControl(r rune) bool {
	if r == '\n' || r == '\t' {
		return false
	}
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// PlainWriter wraps w so everything written through it is Plain.
func PlainWriter(w io.Writer) io.Writer { return plainWriter{w} }

type plainWriter struct{ w io.Writer }

func (p plainWriter) Write(b []byte) (int, error) {
	if _, err := io.WriteString(p.w, Plain(string(b))); err != nil {
		return 0, err
	}
	return len(b), nil
}
