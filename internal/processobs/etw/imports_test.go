package etw

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestUntaggedFilesStayPure pins the portable, untagged half of this package
// (CLAUDE.md section 1): CI has no Windows runner, so parse.go, errors.go,
// session.go and handles.go must never grow a database, HTTP, watcher or
// syscall/unsafe dependency. syscall is allowed in errors.go only, for the
// platform-neutral syscall.Errno type; it is NOT a licence to make a syscall.
func TestUntaggedFilesStayPure(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	var untagged []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		tagged, err := hasBuildConstraint(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if !tagged {
			untagged = append(untagged, f)
		}
	}
	// A guard that silently checked nothing would be worse than none.
	if len(untagged) < 4 {
		t.Fatalf("only %d untagged files found; expected at least parse.go, errors.go, session.go and handles.go", len(untagged))
	}
	importpin.Check(t, importpin.Spec{
		Files: untagged,
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"unsafe",
			"golang.org/x/sys/windows",
			"github.com/fsnotify/fsnotify",
		},
		Custom: func(file, p string) string {
			if p == "syscall" && filepath.Base(file) != "errors.go" {
				return "allowed only in errors.go"
			}
			return ""
		},
		Reason: "the untagged half of internal/processobs/etw must stay portable and testable off Windows",
	})
}

// hasBuildConstraint reports whether a file carries a //go:build line ahead of
// its package clause, i.e. whether it is platform-tagged.
func hasBuildConstraint(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			return false, nil
		}
		if strings.HasPrefix(line, "//go:build") {
			return true, nil
		}
	}
	return false, nil
}
