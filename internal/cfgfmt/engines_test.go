package cfgfmt

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type editStep struct {
	del   bool
	path  Path
	value any
}

// TestEngineEdits pins the exact output of small, targeted edits per
// engine: one row per layout rule.
func TestEngineEdits(t *testing.T) {
	cases := []struct {
		name   string
		format Format
		in     string
		steps  []editStep
		want   string
	}{
		// JSON / JSONC
		{"json-empty-file", FormatJSON, "", []editStep{{path: Path{"a"}, value: 1}}, "{\n  \"a\": 1\n}\n"},
		{"json-empty-object", FormatJSON, "{}\n", []editStep{{path: Path{"a", "b"}, value: true}}, "{\n  \"a\": {\n    \"b\": true\n  }\n}\n"},
		{"json-inline-object", FormatJSON, `{"a": 1}`, []editStep{{path: Path{"b"}, value: "x"}}, `{"a": 1, "b": "x"}`},
		{"json-inline-delete-first", FormatJSON, `{"a": 1, "b": 2}`, []editStep{{del: true, path: Path{"a"}}}, `{"b": 2}`},
		{"json-tab-indent", FormatJSON, "{\n\t\"a\": 1\n}", []editStep{{path: Path{"b"}, value: []any{"x", "y"}}}, "{\n\t\"a\": 1,\n\t\"b\": [\n\t\t\"x\",\n\t\t\"y\"\n\t]\n}"},
		{"json-no-html-escape", FormatJSON, "{}", []editStep{{path: Path{"cmd"}, value: "a && b <c>"}}, "{\n  \"cmd\": \"a && b <c>\"\n}"},
		{"jsonc-comments-only", FormatJSONC, "// my settings\n", []editStep{{path: Path{"a"}, value: 1}}, "// my settings\n{\n  \"a\": 1\n}\n"},
		{"jsonc-same-line-comment-stays", FormatJSONC, "{\n  \"a\": 1 // keep me\n}\n", []editStep{{path: Path{"b"}, value: 2}}, "{\n  \"a\": 1, // keep me\n  \"b\": 2\n}\n"},
		{"jsonc-delete-keeps-prev-comment", FormatJSONC, "{\n  \"a\": 1, // about a\n  \"b\": 2, // about b\n  \"c\": 3\n}\n", []editStep{{del: true, path: Path{"b"}}}, "{\n  \"a\": 1, // about a\n  \"c\": 3\n}\n"},
		{"jsonc-delete-last-drops-its-comment", FormatJSONC, "{\n  \"a\": 1,\n  \"b\": 2 // about b\n}\n", []editStep{{del: true, path: Path{"b"}}}, "{\n  \"a\": 1\n}\n"},
		{"jsonc-delete-only", FormatJSONC, "{\n  \"a\": 1\n}\n", []editStep{{del: true, path: Path{"a"}}}, "{}\n"},
		{"json-null-intermediate", FormatJSON, "{\n  \"a\": null\n}", []editStep{{path: Path{"a", "b"}, value: 1}}, "{\n  \"a\": {\n    \"b\": 1\n  }\n}"},
		{"json-crlf-insert", FormatJSON, "{\r\n  \"a\": 1\r\n}\r\n", []editStep{{path: Path{"b"}, value: 2}}, "{\r\n  \"a\": 1,\r\n  \"b\": 2\r\n}\r\n"},
		// TOML
		{"toml-empty", FormatTOML, "", []editStep{{path: Path{"model"}, value: "x"}}, "model = \"x\"\n"},
		{"toml-multiline-array-replace", FormatTOML, "a = [\n  1, # one\n  2,\n]\nb = 1\n", []editStep{{path: Path{"a"}, value: []any{3}}}, "a = [3]\nb = 1\n"},
		{"toml-multiline-string-replace", FormatTOML, "a = \"\"\"\nline1\nline2\"\"\" # c\nb = 1\n", []editStep{{path: Path{"a"}, value: "x"}}, "a = \"x\" # c\nb = 1\n"},
		{"toml-keep-trailing-comment", FormatTOML, "a = 1 # the answer\n", []editStep{{path: Path{"a"}, value: 2}}, "a = 2 # the answer\n"},
		{"toml-quoted-key", FormatTOML, "", []editStep{{path: Path{"projects", "/home/x y", "trust_level"}, value: "trusted"}}, "[projects.\"/home/x y\"]\ntrust_level = \"trusted\"\n"},
		{"toml-float-int", FormatTOML, "", []editStep{{path: Path{"f"}, value: 2.0}, {path: Path{"i"}, value: 2}}, "f = 2.0\ni = 2\n"},
		{"toml-escape", FormatTOML, "", []editStep{{path: Path{"s"}, value: "a\"b\\c\nd\x01"}}, "s = \"a\\\"b\\\\c\\nd\\u0001\"\n"},
		{"toml-datetime", FormatTOML, "", []editStep{{path: Path{"t"}, value: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)}}, "t = 2026-10-03T12:00:00Z\n"},
		{"toml-insert-into-header-keeps-indent", FormatTOML, "[a]\n  x = 1\n\n[b]\n  y = 2\n", []editStep{{path: Path{"a", "z"}, value: 3}}, "[a]\n  x = 1\n  z = 3\n\n[b]\n  y = 2\n"},
		{"toml-dotted-sub-table", FormatTOML, "[tui]\nnotifications.enabled = true\n", []editStep{{path: Path{"tui", "notifications", "sound"}, value: false}}, "[tui]\nnotifications.enabled = true\nnotifications.sound = false\n"},
		{"toml-super-table", FormatTOML, "[mcp_servers.a]\ncommand = \"x\"\n", []editStep{{path: Path{"mcp_servers", "b", "command"}, value: "y"}}, "[mcp_servers.a]\ncommand = \"x\"\n\n[mcp_servers.b]\ncommand = \"y\"\n"},
		{"toml-delete-last-no-eol", FormatTOML, "a = 1\nb = 2", []editStep{{del: true, path: Path{"b"}}}, "a = 1"},
		{"toml-delete-with-attached-comment", FormatTOML, "a = 1\n# about b\nb = 2\nc = 3\n", []editStep{{del: true, path: Path{"b"}}}, "a = 1\nc = 3\n"},
		{"toml-crlf-insert", FormatTOML, "a = 1\r\n", []editStep{{path: Path{"b"}, value: 2}}, "a = 1\r\nb = 2\r\n"},
		{"toml-no-eol-insert", FormatTOML, "a = 1", []editStep{{path: Path{"b"}, value: 2}}, "a = 1\nb = 2\n"},
		// YAML
		{"yaml-empty", FormatYAML, "", []editStep{{path: Path{"a", "b"}, value: 1}}, "a:\n  b: 1\n"},
		{"yaml-comments-only", FormatYAML, "# settings\n", []editStep{{path: Path{"a"}, value: "x"}}, "# settings\na: x\n"},
		{"yaml-quote-style-kept", FormatYAML, "a: 'x' # c\nb: \"y\"\n", []editStep{{path: Path{"a"}, value: "it's"}, {path: Path{"b"}, value: "z"}}, "a: 'it''s' # c\nb: \"z\"\n"},
		{"yaml-keyword-string-quoted", FormatYAML, "a: x\n", []editStep{{path: Path{"a"}, value: "yes"}}, "a: \"yes\"\n"},
		{"yaml-null-to-map", FormatYAML, "a:\nb: 1\n", []editStep{{path: Path{"a", "c"}, value: 2}}, "a:\n  c: 2\nb: 1\n"},
		{"yaml-flow-map-insert", FormatYAML, "a: {x: 1}\nb: 2\n", []editStep{{path: Path{"a", "y"}, value: 2}}, "a: {\"x\":1,\"y\":2}\nb: 2\n"},
		{"yaml-delete-only-child", FormatYAML, "a:\n  b: 1\nc: 2\n", []editStep{{del: true, path: Path{"a", "b"}}}, "a: {}\nc: 2\n"},
		{"yaml-indent-4", FormatYAML, "a:\n    b: 1\n", []editStep{{path: Path{"a", "c"}, value: map[string]any{"d": 1}}}, "a:\n    b: 1\n    c:\n        d: 1\n"},
		{"yaml-compact-seq", FormatYAML, "a:\n- x\n- y\nb: 1\n", []editStep{{del: true, path: Path{"a"}}}, "b: 1\n"},
		{"yaml-block-scalar-replace", FormatYAML, "a: |\n  one\n  two\nb: 1\n", []editStep{{path: Path{"a"}, value: "x"}}, "a: x\nb: 1\n"},
		{"yaml-multiline-string-value", FormatYAML, "a: x\n", []editStep{{path: Path{"a"}, value: "l1\nl2"}}, "a: \"l1\\nl2\"\n"},
		{"yaml-unicode-column", FormatYAML, "é: \"ü\" # c\n", []editStep{{path: Path{"é"}, value: "ä"}}, "é: \"ä\" # c\n"},
		{"yaml-crlf-insert", FormatYAML, "a: 1\r\n", []editStep{{path: Path{"b"}, value: 2}}, "a: 1\r\nb: 2\r\n"},
		{"yaml-merge-override", FormatYAML, "base: &b\n  x: 1\nuse:\n  <<: *b\n", []editStep{{path: Path{"use", "x"}, value: 2}}, "base: &b\n  x: 1\nuse:\n  <<: *b\n  x: 2\n"},
		// Markdown front matter
		{"md-no-fm-crlf", FormatMarkdown, "# Title\r\n", []editStep{{path: Path{"name"}, value: "x"}}, "---\r\nname: x\r\n---\r\n# Title\r\n"},
		{"md-thematic-break-not-fm", FormatMarkdown, "---\n# Title\n", []editStep{{path: Path{"name"}, value: "x"}}, "---\nname: x\n---\n---\n# Title\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := Parse(c.format, []byte(c.in))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			for _, s := range c.steps {
				if s.del {
					found, err := d.Delete(s.path)
					if err != nil || !found {
						t.Fatalf("delete %s: %v %v", s.path, found, err)
					}
				} else if err := d.Set(s.path, s.value); err != nil {
					t.Fatalf("set %s: %v", s.path, err)
				}
			}
			if got := string(d.Bytes()); got != c.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, c.want)
			}
			if _, err := Parse(c.format, d.Bytes()); err != nil {
				t.Fatalf("output does not re-parse: %v", err)
			}
		})
	}
}

// TestDeleteAbsent: deleting a missing key reports found=false and leaves
// the bytes alone, for every engine.
func TestDeleteAbsent(t *testing.T) {
	for _, c := range []struct {
		f  Format
		in string
	}{
		{FormatJSON, `{"a": 1}`},
		{FormatJSONC, "{}"},
		{FormatTOML, "a = 1\n"},
		{FormatYAML, "a: 1\n"},
		{FormatMarkdown, "# x\n"},
		{FormatMDC, "---\na: 1\n---\n"},
	} {
		d, err := Parse(c.f, []byte(c.in))
		if err != nil {
			t.Fatal(err)
		}
		found, err := d.Delete(Path{"zzz"})
		if err != nil || found {
			t.Fatalf("%s: found=%v err=%v", c.f, found, err)
		}
		if string(d.Bytes()) != c.in {
			t.Fatalf("%s: bytes changed", c.f)
		}
	}
}

// TestNormalize covers the caller-value normalization table.
func TestNormalize(t *testing.T) {
	type myStr string
	cases := []struct {
		in   any
		want any
		err  bool
	}{
		{1, int64(1), false},
		{uint8(3), int64(3), false},
		{float32(1.5), float64(1.5), false},
		{[]string{"a"}, []any{"a"}, false},
		{map[string]int{"a": 1}, map[string]any{"a": int64(1)}, false},
		{myStr("x"), "x", false},
		{uint64(1 << 63), nil, true},
		{struct{}{}, nil, true},
		{map[int]int{1: 1}, nil, true},
	}
	for _, c := range cases {
		got, err := Normalize(c.in)
		if (err != nil) != c.err {
			t.Fatalf("%T %v: err = %v", c.in, c.in, err)
		}
		if !c.err && !Equal(got, c.want) {
			t.Fatalf("%T %v: got %#v want %#v", c.in, c.in, got, c.want)
		}
		if c.err && !errors.Is(err, ErrValueType) {
			t.Fatalf("%T: err = %v, want ErrValueType", c.in, err)
		}
	}
	if !Equal(int64(2), 2.0) || Equal(int64(2), "2") {
		t.Fatal("numeric cross-type equality broken")
	}
}

func TestPathString(t *testing.T) {
	if got := (Path{"a", "b.c", ""}).String(); got != `a."b.c".""` {
		t.Fatalf("got %s", got)
	}
	if !strings.Contains((&ParseError{Format: FormatTOML, Line: 2, Column: 3, Msg: "x"}).Error(), "line 2, column 3") {
		t.Fatal("ParseError message lacks the position")
	}
}
