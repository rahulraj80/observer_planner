package cfgfmt

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestHostileInputs: one row per hostile shape, per format. Every row must
// fail closed with the named error class (never panic, never "succeed"
// with a document whose readers would disagree).
func TestHostileInputs(t *testing.T) {
	deepJSON := strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1)
	deepTOML := "a = " + strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1)
	deepYAML := "a: " + strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1)
	huge := bytes.Repeat([]byte(" "), MaxBytes+1)
	cases := []struct {
		name   string
		format Format
		in     string
		want   error // sentinel (errors.Is), or nil for "*ParseError"
	}{
		{"json-too-large", FormatJSON, string(huge), ErrTooLarge},
		{"json-nul", FormatJSON, "{\"a\":\"x\x00\"}", ErrBinary},
		{"json-bad-utf8", FormatJSONC, "{\"a\":\"\xff\xfe\"}", ErrBinary},
		{"json-depth-bomb", FormatJSON, deepJSON, ErrTooDeep},
		{"json-duplicate-key", FormatJSON, `{"a": 1, "a": 2}`, ErrDuplicateKey},
		{"json-duplicate-nested", FormatJSONC, `{"a": {"b": 1, "b": 2}}`, ErrDuplicateKey},
		{"json-comment-in-strict", FormatJSON, "{\n  // c\n  \"a\": 1\n}", nil},
		{"json-trailing-comma-in-strict", FormatJSON, `{"a": 1,}`, nil},
		{"json-comments-only-strict", FormatJSON, "// only a comment\n", nil},
		{"json-unterminated", FormatJSONC, `{"a": "b`, nil},
		{"json-unterminated-comment", FormatJSONC, "{\"a\": 1 /* never closed", nil},
		{"json-garbage", FormatJSONC, `{"a" 1}`, nil},
		{"toml-too-large", FormatTOML, string(huge), ErrTooLarge},
		{"toml-nul", FormatTOML, "a = \"\x00\"", ErrBinary},
		{"toml-depth-bomb", FormatTOML, deepTOML, ErrTooDeep},
		{"toml-duplicate-key", FormatTOML, "a = 1\na = 2\n", ErrDuplicateKey},
		{"toml-duplicate-table", FormatTOML, "[t]\na = 1\n[t]\nb = 2\n", ErrDuplicateKey},
		{"toml-bad-escape", FormatTOML, `a = "\_"`, nil},
		{"toml-unterminated-multiline", FormatTOML, "a = \"\"\"never closed\n", nil},
		{"toml-unterminated-array", FormatTOML, "a = [1, 2\n", nil},
		{"yaml-too-large", FormatYAML, string(huge), ErrTooLarge},
		{"yaml-nul", FormatYAML, "a: \x00", ErrBinary},
		{"yaml-depth-bomb", FormatYAML, deepYAML, ErrTooDeep},
		{"yaml-duplicate-key", FormatYAML, "a: 1\na: 2\n", ErrDuplicateKey},
		{"yaml-multi-document", FormatYAML, "a: 1\n---\nb: 2\n", ErrUnsupported},
		{"yaml-tab-indent", FormatYAML, "a:\n\tb: 1\n", nil},
		{"yaml-bad-mapping", FormatYAML, "a: b: c\n", nil},
		{"yaml-root-is-list", FormatYAML, "- a\n- b\n", ErrTypeMismatch},
		{"md-front-matter-bad-yaml", FormatMarkdown, "---\na: [\n---\nbody\n", nil},
		{"md-front-matter-duplicate", FormatMDC, "---\na: 1\na: 2\n---\n", ErrDuplicateKey},
		{"md-nul", FormatMarkdown, "# x\x00", ErrBinary},
		{"unknown-format", Format("ini"), "a=1", ErrUnknownFormat},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic: %v", r)
				}
			}()
			_, err := Parse(c.format, []byte(c.in))
			if err == nil {
				t.Fatal("parse succeeded on hostile input")
			}
			if c.want != nil {
				if !errors.Is(err, c.want) {
					t.Fatalf("err = %v, want %v", err, c.want)
				}
				return
			}
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %T %v, want *ParseError", err, err)
			}
		})
	}
}

// TestParseErrorPositions: syntax errors carry a usable position for the
// hygiene pipeline's step 1 message.
func TestParseErrorPositions(t *testing.T) {
	cases := []struct {
		format   Format
		in       string
		wantLine int
	}{
		{FormatJSONC, "{\n  \"a\": 1,\n  \"b\" 2\n}", 3},
		{FormatTOML, "a = 1\nb = \n", 2},
		{FormatYAML, "a: 1\nb: [\n", 3},
		{FormatMarkdown, "---\nname: x\nbad: [\n---\n", 4},
	}
	for _, c := range cases {
		_, err := Parse(c.format, []byte(c.in))
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Fatalf("%s: err = %v", c.format, err)
		}
		if pe.Line == 0 || pe.Line > c.wantLine+1 || pe.Line < c.wantLine-1 {
			t.Errorf("%s: line = %d, want about %d (%v)", c.format, pe.Line, c.wantLine, pe)
		}
	}
}

// TestHostileEdits: edits that would be unsafe are refused with a typed
// error and leave the bytes untouched.
func TestHostileEdits(t *testing.T) {
	cases := []struct {
		name   string
		format Format
		in     string
		del    bool
		path   Path
		value  any
		want   error
	}{
		{"json-path-through-scalar", FormatJSON, `{"a": 1}`, false, Path{"a", "b"}, 2, ErrTypeMismatch},
		{"json-path-through-array", FormatJSON, `{"a": [1]}`, false, Path{"a", "0"}, 2, ErrTypeMismatch},
		{"json-nan", FormatJSON, `{}`, false, Path{"a"}, nanValue(), ErrValueType},
		{"json-func-value", FormatJSON, `{}`, false, Path{"a"}, func() {}, ErrValueType},
		{"json-empty-path", FormatJSON, `{}`, false, Path{}, 1, ErrEmptyPath},
		{"json-root-array", FormatJSON, `[1]`, false, Path{"a"}, 1, ErrTypeMismatch},
		{"toml-null", FormatTOML, "a = 1\n", false, Path{"b"}, nil, ErrValueType},
		{"toml-path-through-scalar", FormatTOML, "a = 1\n", false, Path{"a", "b"}, 2, ErrTypeMismatch},
		{"yaml-anchor-target", FormatYAML, "base: &b\n  x: 1\nuse: *b\n", false, Path{"base", "x"}, 2, ErrUnsupported},
		{"yaml-anchor-delete", FormatYAML, "base: &b 1\nuse: *b\n", true, Path{"base"}, nil, ErrUnsupported},
		{"yaml-through-alias", FormatYAML, "base: &b\n  x: 1\nuse: *b\n", false, Path{"use", "x"}, 2, ErrUnsupported},
		{"yaml-delete-merged-key", FormatYAML, "base: &b\n  x: 1\nuse:\n  <<: *b\n  y: 2\n", true, Path{"use", "x"}, nil, ErrUnsupported},
		{"yaml-path-through-scalar", FormatYAML, "a: 1\n", false, Path{"a", "b"}, 2, ErrTypeMismatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := Parse(c.format, []byte(c.in))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if c.del {
				_, err = d.Delete(c.path)
			} else {
				err = d.Set(c.path, c.value)
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if got := string(d.Bytes()); got != c.in {
				t.Fatalf("bytes changed after a refused edit:\n%s", got)
			}
		})
	}
}

func nanValue() float64 {
	zero := 0.0
	return zero / zero
}
