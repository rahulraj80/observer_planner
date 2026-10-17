package cfgfmt

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/cfgfmt/golden/*.golden from the engines")

const fixtureDir = "../../testdata/cfgfmt"

// fixtureFormat maps a fixture directory to its engine.
var fixtureFormat = map[string]Format{
	"json":     FormatJSON,
	"jsonc":    FormatJSONC,
	"toml":     FormatTOML,
	"yaml":     FormatYAML,
	"markdown": FormatMarkdown,
	"mdc":      FormatMDC,
}

type fixture struct {
	name   string // "toml/codex-config-sample.toml"
	format Format
	data   []byte
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	var out []fixture
	for dir, f := range fixtureFormat {
		matches, err := filepath.Glob(filepath.Join(fixtureDir, dir, "*"))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			b, err := os.ReadFile(m)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, fixture{name: dir + "/" + filepath.Base(m), format: f, data: b})
		}
	}
	if len(out) < 10 {
		t.Fatalf("only %d fixtures found under %s", len(out), fixtureDir)
	}
	return out
}

func loadFixture(t *testing.T, name string) fixture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return fixture{name: name, format: fixtureFormat[strings.SplitN(name, "/", 2)[0]], data: b}
}

// TestGoldenRoundTrip: every real vendor fixture parses and renders back
// byte for byte, also as CRLF, with a BOM, and without a final newline.
func TestGoldenRoundTrip(t *testing.T) {
	for _, fx := range loadFixtures(t) {
		variants := map[string][]byte{
			"lf":        fx.data,
			"crlf":      bytes.ReplaceAll(fx.data, []byte("\n"), []byte("\r\n")),
			"bom":       append(append([]byte{}, utf8BOM...), fx.data...),
			"no-eol":    bytes.TrimRight(fx.data, "\n"),
			"mixed-eol": mixEOL(fx.data),
		}
		for vname, in := range variants {
			t.Run(fx.name+"/"+vname, func(t *testing.T) {
				d, err := Parse(fx.format, in)
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				if got := d.Bytes(); !bytes.Equal(got, in) {
					t.Fatalf("round trip changed the bytes:\n%s", firstDiff(string(in), string(got)))
				}
				if _, err := d.Value(); err != nil {
					t.Fatalf("value: %v", err)
				}
			})
		}
	}
}

// mixEOL turns every other newline into CRLF.
func mixEOL(b []byte) []byte {
	var out []byte
	n := 0
	for _, c := range b {
		if c == '\n' {
			if n%2 == 1 {
				out = append(out, '\r')
			}
			n++
		}
		out = append(out, c)
	}
	return out
}

type goldenOp struct {
	del   bool
	path  Path
	value any
}

type goldenCase struct {
	name    string
	fixture string
	ops     []goldenOp
	// maxHunks bounds how many separate places in the file may change.
	maxHunks int
	// disclosed is true when the edit must fall back (and say so).
	disclosed bool
}

func set(p Path, v any) goldenOp { return goldenOp{path: p, value: v} }
func del(p Path) goldenOp        { return goldenOp{del: true, path: p} }

var goldenCases = []goldenCase{
	// JSON (strict): Claude Code and Gemini CLI vendor examples.
	{"claude-insert-root", "json/claude-code-settings.json", []goldenOp{set(Path{"model"}, "opus")}, 1, false},
	{"claude-insert-nested", "json/claude-code-settings.json", []goldenOp{set(Path{"permissions", "defaultMode"}, "acceptEdits")}, 1, false},
	{"claude-replace-array", "json/claude-code-settings.json", []goldenOp{set(Path{"permissions", "allow"}, []any{"Bash(npm run lint)", "Bash(npm run test *)", "Read(./src/**)"})}, 1, false},
	{"claude-delete-object", "json/claude-code-settings.json", []goldenOp{del(Path{"permissions", "deny"})}, 1, false},
	{"claude-new-object-path", "json/claude-code-settings.json", []goldenOp{set(Path{"env", "CLAUDE_CODE_MAX_OUTPUT_TOKENS"}, "32000")}, 1, false},
	{"gemini-replace-scalar", "json/gemini-cli-settings.json", []goldenOp{set(Path{"ui", "theme"}, "Default")}, 1, false},
	{"gemini-replace-deep", "json/gemini-cli-settings.json", []goldenOp{set(Path{"general", "sessionRetention", "maxAge"}, "7d")}, 1, false},
	{"gemini-insert-float", "json/gemini-cli-settings.json", []goldenOp{set(Path{"model", "compressionThreshold"}, 0.6)}, 1, false},
	{"gemini-delete-section", "json/gemini-cli-settings.json", []goldenOp{del(Path{"telemetry"})}, 1, false},
	{"gemini-delete-last", "json/gemini-cli-settings.json", []goldenOp{del(Path{"advanced"})}, 1, false},
	// JSONC: OpenCode examples (trailing commas, comments).
	{"opencode-replace-in-trailing-comma", "jsonc/opencode.jsonc", []goldenOp{set(Path{"server", "port"}, 4097)}, 1, false},
	{"opencode-insert-root-trailing-comma", "jsonc/opencode.jsonc", []goldenOp{set(Path{"small_model"}, "anthropic/claude-haiku-4-5")}, 1, false},
	{"opencode-delete-root", "jsonc/opencode.jsonc", []goldenOp{del(Path{"autoupdate"})}, 1, false},
	{"opencode-insert-after-comment", "jsonc/opencode-tui.jsonc", []goldenOp{set(Path{"agent", "code-reviewer", "tools", "bash"}, false)}, 1, false},
	{"opencode-delete-commented", "jsonc/opencode-tui.jsonc", []goldenOp{del(Path{"agent", "code-reviewer", "tools", "write"})}, 1, false},
	// TOML: the Codex sample config (comment-heavy) and an Open Interpreter config.
	{"codex-replace-model", "toml/codex-config-sample.toml", []goldenOp{set(Path{"model"}, "gpt-6.1-sol-mini")}, 1, false},
	{"codex-insert-root", "toml/codex-config-sample.toml", []goldenOp{set(Path{"model_reasoning_effort"}, "high")}, 1, false},
	{"codex-replace-in-table", "toml/codex-config-sample.toml", []goldenOp{set(Path{"history", "persistence"}, "none")}, 1, false},
	{"codex-insert-in-table", "toml/codex-config-sample.toml", []goldenOp{set(Path{"sandbox_workspace_write", "writable_roots"}, []any{"/tmp/build"})}, 1, false},
	{"codex-new-table", "toml/codex-config-sample.toml", []goldenOp{set(Path{"tui", "theme"}, "dark")}, 1, false},
	{"codex-new-mcp-server", "toml/codex-config-sample.toml", []goldenOp{set(Path{"mcp_servers", "docs"}, map[string]any{"command": "npx", "args": []any{"-y", "docs-mcp"}})}, 1, false},
	{"codex-delete-root", "toml/codex-config-sample.toml", []goldenOp{del(Path{"model_provider"})}, 1, false},
	{"codex-delete-table", "toml/codex-config-sample.toml", []goldenOp{del(Path{"shell_environment_policy", "filters"})}, 1, false},
	{"codex-replace-inline-table-key", "toml/codex-config-sample.toml", []goldenOp{set(Path{"shell_environment_policy", "set"}, map[string]any{"CI": "1"})}, 1, false},
	{"oi-replace-quoted-table", "toml/openinterpreter-config-wsl.toml", []goldenOp{set(Path{"projects", "/home/dev/example-project", "trust_level"}, "untrusted")}, 1, false},
	{"oi-insert-root-before-table", "toml/openinterpreter-config-wsl.toml", []goldenOp{set(Path{"model"}, "gpt-6")}, 1, false},
	// YAML: aider (all comments), hermes (comment-heavy), goose (docs example).
	{"aider-insert-into-comments-only", "yaml/aider-sample.conf.yml", []goldenOp{set(Path{"model"}, "sonnet")}, 1, false},
	{"hermes-replace-quoted-keeps-comment", "yaml/hermes-cli-config.yaml", []goldenOp{set(Path{"database", "journal_mode"}, "delete")}, 1, false},
	{"hermes-replace-int", "yaml/hermes-cli-config.yaml", []goldenOp{set(Path{"runtime", "nofile_soft_limit"}, 8192)}, 1, false},
	{"hermes-delete-section", "yaml/hermes-cli-config.yaml", []goldenOp{del(Path{"attachments"})}, 1, false},
	{"goose-replace-quoted", "yaml/goose-config.yaml", []goldenOp{set(Path{"GOOSE_MODE"}, "approve")}, 1, false},
	{"goose-replace-block-seq", "yaml/goose-config.yaml", []goldenOp{set(Path{"GOOSE_SEARCH_PATHS"}, []any{"/usr/local/bin"})}, 1, false},
	{"goose-replace-nested-bool", "yaml/goose-config.yaml", []goldenOp{set(Path{"extensions", "memory", "enabled"}, false)}, 1, false},
	{"goose-insert-map", "yaml/goose-config.yaml", []goldenOp{set(Path{"extensions", "observer"}, map[string]any{"enabled": true, "type": "stdio", "cmd": "observer", "args": []any{"mcp"}})}, 1, false},
	{"goose-delete-nested", "yaml/goose-config.yaml", []goldenOp{del(Path{"extensions", "developer"})}, 1, false},
	{"goose-insert-root", "yaml/goose-config.yaml", []goldenOp{set(Path{"GOOSE_TELEMETRY_ENABLED"}, false)}, 1, false},
	// Markdown / MDC front matter.
	{"subagent-replace-model", "markdown/claude-code-subagent.md", []goldenOp{set(Path{"model"}, "opus")}, 1, false},
	{"subagent-insert-key", "markdown/claude-code-subagent.md", []goldenOp{set(Path{"color"}, "blue")}, 1, false},
	{"skill-replace-description", "markdown/skill-creator-SKILL.md", []goldenOp{set(Path{"description"}, "Create and improve skills.")}, 1, false},
	{"gemini-md-create-front-matter", "markdown/gemini-cli-GEMINI.md", []goldenOp{set(Path{"description"}, "Project instructions")}, 1, false},
	{"mdc-replace-bool", "mdc/anti-overengineering.mdc", []goldenOp{set(Path{"alwaysApply"}, true)}, 1, false},
	{"mdc-replace-flow-seq", "mdc/anti-overengineering.mdc", []goldenOp{set(Path{"globs"}, []any{"src/**"})}, 1, false},
	// Disclosed fallbacks: an edit inside an array of tables re-serializes.
	{"codex-array-of-tables", "toml/codex-config-sample.toml", []goldenOp{set(Path{"skills", "config"}, []any{map[string]any{"path": "/skills/a", "enabled": false}})}, 0, true},
}

// TestGoldenEdits applies single-key edits to real vendor files and checks
// (1) the decoded tree changed by exactly that edit, (2) everything else
// in the file is byte-identical (the change is confined to at most
// maxHunks contiguous regions), (3) the output matches the reviewed golden.
func TestGoldenEdits(t *testing.T) {
	for _, tc := range goldenCases {
		t.Run(tc.name, func(t *testing.T) {
			fx := loadFixture(t, tc.fixture)
			d, err := Parse(fx.format, fx.data)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			expected, err := d.Value()
			if err != nil {
				t.Fatal(err)
			}
			for _, op := range tc.ops {
				if op.del {
					found, err := d.Delete(op.path)
					if err != nil || !found {
						t.Fatalf("delete %s: found=%v err=%v", op.path, found, err)
					}
					expected = treeDelete(expected, op.path)
					continue
				}
				if err := d.Set(op.path, op.value); err != nil {
					t.Fatalf("set %s: %v", op.path, err)
				}
				nv, _ := Normalize(op.value)
				expected, _ = treeSet(expected, op.path, nv)
				got, found, err := d.Get(op.path)
				if err != nil || !found || !Equal(got, nv) {
					t.Fatalf("get %s after set = %v, %v, %v", op.path, got, found, err)
				}
			}
			out := d.Bytes()
			// Re-parse the output from scratch: it must be valid and carry
			// exactly the expected tree.
			d2, err := Parse(fx.format, out)
			if err != nil {
				t.Fatalf("output does not re-parse: %v\n%s", err, out)
			}
			got, err := d2.Value()
			if err != nil || !Equal(any(got), any(expected)) {
				t.Fatalf("output tree differs from expected (err=%v)", err)
			}
			if tc.disclosed != (len(d.Disclosures()) > 0) {
				t.Fatalf("disclosures = %v, want disclosed=%v", d.Disclosures(), tc.disclosed)
			}
			if !tc.disclosed {
				hunks := diffHunks(splitTextLines(string(fx.data)), splitTextLines(string(out)))
				if len(hunks) == 0 || len(hunks) > tc.maxHunks {
					t.Fatalf("edit touched %d regions (want 1..%d):\n%s", len(hunks), tc.maxHunks, renderHunks(hunks))
				}
			}
			golden := filepath.Join(fixtureDir, "golden", tc.name+".golden")
			if *update {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, out, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("missing golden (run with -update and review): %v", err)
			}
			if !bytes.Equal(out, want) {
				t.Fatalf("output differs from golden:\n%s", firstDiff(string(want), string(out)))
			}
		})
	}
}

// TestGoldenEditsCRLF: the same edits on a CRLF copy produce the LF golden
// with CRLF terminators (inserted lines follow the file's line endings).
func TestGoldenEditsCRLF(t *testing.T) {
	for _, tc := range goldenCases {
		if tc.disclosed {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			fx := loadFixture(t, tc.fixture)
			in := bytes.ReplaceAll(fx.data, []byte("\n"), []byte("\r\n"))
			d, err := Parse(fx.format, in)
			if err != nil {
				t.Fatal(err)
			}
			for _, op := range tc.ops {
				if op.del {
					if _, err := d.Delete(op.path); err != nil {
						t.Fatal(err)
					}
				} else if err := d.Set(op.path, op.value); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(filepath.Join(fixtureDir, "golden", tc.name+".golden"))
			if err != nil {
				t.Skip("golden not generated yet")
			}
			wantCRLF := bytes.ReplaceAll(want, []byte("\n"), []byte("\r\n"))
			if got := d.Bytes(); !bytes.Equal(got, wantCRLF) {
				t.Fatalf("CRLF edit differs:\n%s", firstDiff(string(wantCRLF), string(got)))
			}
		})
	}
}

// TestForbidReserialize: with the option set, an edit that would need a
// re-serialization fails loudly and leaves the bytes untouched.
func TestForbidReserialize(t *testing.T) {
	fx := loadFixture(t, "toml/codex-config-sample.toml")
	d, err := ParseWith(FormatTOML, fx.data, Options{ForbidReserialize: true})
	if err != nil {
		t.Fatal(err)
	}
	err = d.Set(Path{"skills", "config"}, []any{map[string]any{"path": "/x", "enabled": true}})
	if !errors.Is(err, ErrNeedsReserialize) {
		t.Fatalf("err = %v, want ErrNeedsReserialize", err)
	}
	if !bytes.Equal(d.Bytes(), fx.data) {
		t.Fatal("bytes changed after a refused edit")
	}
}

// TestIdempotentSet: setting the current value is a byte-level no-op.
func TestIdempotentSet(t *testing.T) {
	cases := []struct {
		fixture string
		path    Path
	}{
		{"json/gemini-cli-settings.json", Path{"ui", "theme"}},
		{"jsonc/opencode.jsonc", Path{"server", "port"}},
		{"toml/codex-config-sample.toml", Path{"model"}},
		{"toml/codex-config-sample.toml", Path{"shell_environment_policy", "filters"}},
		{"yaml/hermes-cli-config.yaml", Path{"database", "journal_mode"}},
		{"yaml/goose-config.yaml", Path{"extensions"}},
		{"markdown/claude-code-subagent.md", Path{"tools"}},
	}
	for _, c := range cases {
		fx := loadFixture(t, c.fixture)
		d, err := Parse(fx.format, fx.data)
		if err != nil {
			t.Fatal(err)
		}
		v, found, err := d.Get(c.path)
		if err != nil || !found {
			t.Fatalf("%s %s: get %v %v", c.fixture, c.path, found, err)
		}
		if err := d.Set(c.path, v); err != nil {
			t.Fatalf("%s %s: %v", c.fixture, c.path, err)
		}
		if !bytes.Equal(d.Bytes(), fx.data) {
			t.Fatalf("%s %s: no-op set changed the bytes", c.fixture, c.path)
		}
	}
}

// --- tiny line diff ------------------------------------------------------

type hunk struct {
	aStart, aEnd, bStart, bEnd int
	a, b                       []string
}

func splitTextLines(s string) []string {
	return strings.SplitAfter(s, "\n")
}

// diffHunks returns the regions where a and b differ (LCS-based).
func diffHunks(a, b []string) []hunk {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out []hunk
	i, j := 0, 0
	var cur *hunk
	flush := func() {
		if cur != nil {
			cur.aEnd, cur.bEnd = i, j
			cur.a, cur.b = a[cur.aStart:i], b[cur.bStart:j]
			out = append(out, *cur)
			cur = nil
		}
	}
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			flush()
			i++
			j++
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			if cur == nil {
				cur = &hunk{aStart: i, bStart: j}
			}
			j++
		default:
			if cur == nil {
				cur = &hunk{aStart: i, bStart: j}
			}
			i++
		}
	}
	flush()
	return out
}

func renderHunks(hs []hunk) string {
	var b strings.Builder
	for _, h := range hs {
		b.WriteString("@@\n")
		for _, l := range h.a {
			b.WriteString("- " + l)
		}
		for _, l := range h.b {
			b.WriteString("+ " + l)
		}
	}
	return b.String()
}

func firstDiff(want, got string) string {
	hs := diffHunks(splitTextLines(want), splitTextLines(got))
	if len(hs) > 3 {
		hs = hs[:3]
	}
	return renderHunks(hs)
}
