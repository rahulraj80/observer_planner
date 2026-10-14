package main

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestMatchRule(t *testing.T) {
	cases := []struct {
		pattern, rel string
		want         bool
	}{
		{"go.mod", "go.mod", true},
		{"go.mod", "x/go.mod", false},
		{"scripts/ci/**", "scripts/ci/go-test-run.sh", true},
		{"scripts/ci/**", "scripts/ci/cipkgs/main.go", true},
		{"**/migrations/**", "internal/db/migrations/136_x.sql", true},
		{"**/migrations/**", "internal/orgserver/db/migrations/175_x.sql", true},
		{"**/migrations/**", "internal/db/db.go", false},
		{"testdata/**", "testdata/claudecode/a.jsonl", true},
		{"testdata/**", "internal/adapter/codex/testdata/a.jsonl", false},
		{"docs/plans/*.md", "docs/plans/x.md", true},
		{"docs/plans/*.md", "docs/plans/agent-access-research/fixtures/a.json", false},
		{".github/workflows/*.yml", ".github/workflows/npm-release.yml", true},
	}
	for _, c := range cases {
		if got := matchRule(c.pattern, c.rel); got != c.want {
			t.Errorf("matchRule(%q, %q) = %v, want %v", c.pattern, c.rel, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	pkgs := map[string]bool{"internal/proxy": true, "internal/adapter/codex": true, "cmd/observer": true, "tests/invariant": true}
	cases := []struct {
		name     string
		changed  []string
		wantAll  bool
		wantDirs []string
	}{
		{"go file maps to its package", []string{"internal/proxy/proxy.go"}, false, []string{"internal/proxy"}},
		{"package testdata maps to the package", []string{"internal/adapter/codex/testdata/s.jsonl"}, false, []string{"internal/adapter/codex"}},
		{"unread doc is ignored", []string{"docs/handovers/x.md", "internal/proxy/a.go"}, false, []string{"internal/proxy"}},
		{"go.sum falls back", []string{"go.sum"}, true, nil},
		{"ci workflow falls back", []string{".github/workflows/ci.yml"}, true, nil},
		{"migration falls back", []string{"internal/orgserver/db/migrations/176_x.sql"}, true, nil},
		{"shared testdata falls back", []string{"testdata/hermes/a.db"}, true, nil},
		{"read doc outside packages falls back", []string{"docs/guard-rules.md"}, true, nil},
		{"other workflow does not", []string{".github/workflows/npm-release.yml"}, false, nil},
		{"invariant test maps to its own package", []string{"tests/invariant/x_test.go"}, false, []string{"tests/invariant"}},
	}
	for _, c := range cases {
		dirs, all, _ := classify(c.changed, pkgs)
		if all != c.wantAll {
			t.Errorf("%s: all = %v, want %v", c.name, all, c.wantAll)
			continue
		}
		if len(dirs) != len(c.wantDirs) {
			t.Errorf("%s: dirs = %v, want %v", c.name, dirs, c.wantDirs)
			continue
		}
		for _, d := range c.wantDirs {
			if !dirs[d] {
				t.Errorf("%s: missing %s in %v", c.name, d, dirs)
			}
		}
	}
}

func TestFileUsesConcurrency(t *testing.T) {
	cases := []struct {
		name, src string
		want      bool
	}{
		{"plain", "package p\nfunc F(a int) int { return a + 1 }\n", false},
		{"go statement", "package p\nfunc F() { go func() {}() }\n", true},
		{"select", "package p\nfunc F(c chan int) { select { case <-c: } }\n", true},
		{"channel type", "package p\ntype T struct{ c chan int }\n", true},
		{"send", "package p\nfunc F(c chan<- int) { c <- 1 }\n", true},
		{"sync import", "package p\nimport \"sync\"\nvar mu sync.Mutex\n", true},
		{"atomic import", "package p\nimport \"sync/atomic\"\nvar n atomic.Int64\n", true},
		{"unrelated import", "package p\nimport \"strings\"\nvar s = strings.ToUpper(\"a\")\n", false},
	}
	for _, c := range cases {
		f, err := parser.ParseFile(token.NewFileSet(), "x.go", c.src, 0)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := fileUsesConcurrency(f); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
