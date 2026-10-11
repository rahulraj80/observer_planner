package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// T-407 (D-298): `observer start` auto-registers hooks on every start. When
// the hooks are already current that pass must not touch the user's file at
// all (it used to rewrite Claude Code's settings.json with the same JSON,
// turning CRLF into LF and changing its hash), and when a write is needed it
// must keep the file's line endings. Every case runs in a throwaway HOME.

// t407Writers are the two JSON write paths: the credential-file splice
// (Claude Code's settings.json) and the re-rendering writer (Cursor's
// hooks.json). Seed is a pre-existing file that still needs the hooks.
var t407Writers = []struct {
	name string
	tool string
	rel  string
	seed string
}{
	{
		name: "claude-code settings.json (credential splice)",
		tool: "claude-code",
		rel:  filepath.Join(".claude", "settings.json"),
		seed: "{\n  \"env\": {\n    \"SOME_KEY\": \"value\"\n  },\n  \"model\": \"opus\"\n}\n",
	},
	{
		name: "cursor hooks.json (re-rendering writer)",
		tool: "cursor",
		rel:  filepath.Join(".cursor", "hooks.json"),
		seed: "{\n  \"version\": 1\n}\n",
	},
}

func t407Registry(t *testing.T) *Registry {
	t.Helper()
	home := t.TempDir()
	r, err := NewRegistry(Options{
		BinaryPath:    "/opt/observer/bin/observer",
		HomeDir:       home,
		ChecksumsPath: filepath.Join(home, ".observer", "hook_checksums.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func toCRLF(b []byte) []byte {
	return bytes.ReplaceAll(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
}

func hasBareLF(b []byte) bool {
	return bytes.Count(b, []byte("\n")) != bytes.Count(b, []byte("\r\n"))
}

// writeAged writes body and backdates its mtime, so a rewrite (even one
// that writes identical bytes) is visible as a changed mtime.
func writeAged(t *testing.T, path string, body []byte) time.Time {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	return old
}

func mustRegister(t *testing.T, r *Registry, tool string) RegistrationResult {
	t.Helper()
	res := r.Register(tool)
	if res.Error != nil {
		t.Fatalf("Register(%s): %v", tool, res.Error)
	}
	return res
}

func TestT407RegisterIsByteIdempotent(t *testing.T) {
	t.Parallel()
	for _, w := range t407Writers {
		for _, crlf := range []bool{false, true} {
			name := w.name + " LF"
			if crlf {
				name = w.name + " CRLF"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				r := t407Registry(t)
				path := filepath.Join(r.opts.HomeDir, w.rel)
				writeAged(t, path, []byte(w.seed))
				mustRegister(t, r, w.tool)
				current, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if crlf {
					current = toCRLF(current)
				}
				old := writeAged(t, path, current)

				res := mustRegister(t, r, w.tool)
				if len(res.HooksAdded) != 0 {
					t.Fatalf("second register added %v; want every hook already set", res.HooksAdded)
				}
				after, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(after, current) {
					t.Errorf("unchanged hooks rewrote the file:\nbefore %q\nafter  %q", current, after)
				}
				fi, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if !fi.ModTime().Equal(old) {
					t.Errorf("unchanged hooks touched the file (mtime %v, want %v)", fi.ModTime(), old)
				}
			})
		}
	}
}

func TestT407RegisterKeepsCRLFWhenItWrites(t *testing.T) {
	t.Parallel()
	for _, w := range t407Writers {
		t.Run(w.name, func(t *testing.T) {
			t.Parallel()
			r := t407Registry(t)
			path := filepath.Join(r.opts.HomeDir, w.rel)
			writeAged(t, path, toCRLF([]byte(w.seed)))

			res := mustRegister(t, r, w.tool)
			if len(res.HooksAdded) == 0 {
				t.Fatal("register added no hooks; the case needs a write")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if hasBareLF(after) {
				t.Errorf("a CRLF file came back with LF line breaks:\n%q", after)
			}
			var doc map[string]json.RawMessage
			if err := json.Unmarshal(after, &doc); err != nil {
				t.Fatalf("not JSON after register: %v", err)
			}
			if _, ok := doc["hooks"]; !ok {
				t.Errorf("hooks missing after register: %s", after)
			}
		})
	}
}

func TestT407RegisterKeepsLFWhenItWrites(t *testing.T) {
	t.Parallel()
	for _, w := range t407Writers {
		t.Run(w.name, func(t *testing.T) {
			t.Parallel()
			r := t407Registry(t)
			path := filepath.Join(r.opts.HomeDir, w.rel)
			writeAged(t, path, []byte(w.seed))
			mustRegister(t, r, w.tool)
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(after, []byte("\r")) {
				t.Errorf("an LF file came back with carriage returns:\n%q", after)
			}
		})
	}
}
