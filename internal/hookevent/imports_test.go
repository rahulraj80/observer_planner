package hookevent

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestNoForbiddenImports pins hookevent as pure data: no SQL, no HTTP, no
// fsnotify, no subprocess, and of this module only the pure
// internal/integration registry.
func TestNoForbiddenImports(t *testing.T) {
	const modulePrefix = "github.com/marmutapp/superbased-observer/"
	allowedInternal := map[string]bool{modulePrefix + "internal/integration": true}
	forbidden := []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify", "os/exec"}
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		checked++
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range af.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if p == bad {
					t.Errorf("%s imports forbidden %q", f, bad)
				}
			}
			if strings.HasPrefix(p, modulePrefix) && !allowedInternal[p] {
				t.Errorf("%s imports %q: hookevent may import only internal/integration", f, p)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test files checked")
	}
}

// TestFromBinding copies the binding's capability facts.
func TestFromBinding(t *testing.T) {
	b := integration.HookBinding{
		Cap: integration.HookCapToolGate, Phase: integration.HookPhasePre,
		NativeEvent: "PreToolUse", ProcRel: integration.ProcDescendant,
	}
	ev := FromBinding("qwen-code", b)
	if ev.Cap != b.Cap || ev.Phase != b.Phase || ev.Tool != "qwen-code" || ev.NativeEvent != "PreToolUse" || ev.ProcRel != b.ProcRel {
		t.Errorf("FromBinding = %+v", ev)
	}
	if Allow().Decision != DecisionAllow {
		t.Error("Allow() is not an allow")
	}
}
