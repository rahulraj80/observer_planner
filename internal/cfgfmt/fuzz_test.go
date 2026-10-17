package cfgfmt

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// fuzzRoundTrip is the shared property: whatever parses renders back byte
// for byte, and a Set either fails with an error or yields a document that
// re-parses and holds the value. Never a panic.
func fuzzRoundTrip(f *testing.F, format Format, dir string, extra ...string) {
	matches, _ := filepath.Glob(filepath.Join(fixtureDir, dir, "*"))
	for _, m := range matches {
		if b, err := os.ReadFile(m); err == nil {
			f.Add(b, "fuzzkey")
		}
	}
	for _, e := range extra {
		f.Add([]byte(e), "k")
	}
	f.Fuzz(func(t *testing.T, in []byte, key string) {
		d, err := Parse(format, in)
		if err != nil {
			return
		}
		if !bytes.Equal(d.Bytes(), in) {
			t.Fatalf("round trip changed the bytes")
		}
		if key == "" {
			return
		}
		if err := d.Set(Path{key}, "v"); err != nil {
			return
		}
		out := d.Bytes()
		d2, err := Parse(format, out)
		if err != nil {
			t.Fatalf("edited output does not re-parse: %v\n%q", err, out)
		}
		if v, found, err := d2.Get(Path{key}); err != nil || !found || v != "v" {
			t.Fatalf("edited value lost: %v %v %v", v, found, err)
		}
	})
}

func FuzzJSONC(f *testing.F) {
	fuzzRoundTrip(f, FormatJSONC, "jsonc", "{}", "{\"a\":[1,{\"b\":null}],}", "// x\n")
}

func FuzzTOML(f *testing.F) {
	fuzzRoundTrip(f, FormatTOML, "toml", "a = [\n 1,\n]\n[t]\nx = '''\ny'''\n", "[[a]]\nb = 1\n")
}

func FuzzYAML(f *testing.F) {
	fuzzRoundTrip(f, FormatYAML, "yaml", "a: &x 1\nb: *x\n", "a: |\n  t\nb:\n- 1\n")
}

func FuzzMarkdown(f *testing.F) {
	fuzzRoundTrip(f, FormatMarkdown, "markdown", "---\na: 1\n---\n<!-- observer:org-begin -->\n<!-- observer:org-end -->\n")
}
