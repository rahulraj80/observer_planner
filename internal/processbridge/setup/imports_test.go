package setup

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/processbridge/setup as a planner, not a
// subsystem (CLAUDE.md section 1): its only I/O is behind the Env seam.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify"},
		Reason:    "internal/processbridge/setup must stay a planner",
	})
}

// TestPlannerIsPure is the stronger gate: plan.go (the planner, PlanTask and
// every helper it calls) may import only a CLOSED allow-list, so a resolved
// input is never re-resolved inside the decision. Anything that can reach the
// filesystem, the process table or the network belongs behind the Env seam.
func TestPlannerIsPure(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Files:     []string{"plan.go"},
		AllowOnly: true,
		Allowed: []string{
			"errors",
			"fmt",
			"net", // net.SplitHostPort/JoinHostPort: string surgery, no dialling.
			"strings",
			"github.com/marmutapp/superbased-observer/internal/processobs/bridge", // DefaultListenAddr, a const.
		},
		Reason: "I/O belongs behind the Env seam in env.go, never in the planner",
	})
}

// TestNoBuildTags pins the build-tag-free property, which is load-bearing
// rather than incidental: the WSL daemon that PLANS the elevated Windows
// command does not run on Windows. A `//go:build windows` anywhere in this
// package would compile the planner out of the exact binary that needs it.
func TestNoBuildTags(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	for _, f := range matches {
		af, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, group := range af.Comments {
			for _, c := range group.List {
				if strings.HasPrefix(c.Text, "//go:build") || strings.HasPrefix(c.Text, "// +build") {
					t.Errorf("%s carries a build constraint (%s) — this package must build "+
						"everywhere, because the host that plans the Windows command is not Windows", f, c.Text)
				}
			}
		}
		// A GOOS/GOARCH filename suffix is the other way to constrain a file.
		base := strings.TrimSuffix(f, ".go")
		for _, suffix := range []string{"_windows", "_linux", "_darwin", "_js", "_wasm"} {
			if strings.HasSuffix(base, suffix) {
				t.Errorf("%s uses a GOOS filename constraint — this package must build everywhere", f)
			}
		}
	}
}
