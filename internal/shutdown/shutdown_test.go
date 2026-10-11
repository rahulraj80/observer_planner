package shutdown

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTrackerRunningAndStage(t *testing.T) {
	tr := NewTracker()
	if got := tr.Stage(); got != StageRunning {
		t.Fatalf("stage = %q, want %q", got, StageRunning)
	}
	doneW := tr.Start("watcher")
	doneA := tr.Start("advisor")
	doneA2 := tr.Start("advisor")
	if got, want := tr.Running(), []string{"advisor x2", "watcher"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("running = %v, want %v", got, want)
	}
	doneA()
	doneA() // a second call is a no-op, not a second decrement
	if got, want := tr.Running(), []string{"advisor", "watcher"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("running = %v, want %v", got, want)
	}
	doneA2()
	doneW()
	if got := tr.Running(); len(got) != 0 {
		t.Fatalf("running = %v, want none", got)
	}
	if !tr.AdvanceStage(StageRunning, StageComponents) {
		t.Fatal("AdvanceStage from running refused")
	}
	tr.SetStage(StageCheckpoint)
	if tr.AdvanceStage(StageRunning, StageComponents) {
		t.Fatal("AdvanceStage rewound a later stage")
	}
	if got := tr.Stage(); got != StageCheckpoint {
		t.Fatalf("stage = %q, want %q", got, StageCheckpoint)
	}
}

func TestReportBlocking(t *testing.T) {
	cases := []struct {
		name string
		r    Report
		want string
	}{
		{"components", Report{Stage: StageComponents, Running: []string{"codeintel", "watcher"}}, "codeintel, watcher"},
		{"cleanup after components", Report{Stage: StageClosing}, "stage closing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Blocking(); got != tc.want {
				t.Fatalf("Blocking() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestWatchdogOverrunNamesTheBlocker: a component that ignores the stop is
// named in the overrun report, and the progress report comes first.
func TestWatchdogOverrunNamesTheBlocker(t *testing.T) {
	tr := NewTracker()
	done := tr.Start("watcher")
	defer done()
	tr.Start("proxy")() // returned promptly
	tr.SetStage(StageComponents)
	prog := make(chan Report, 1)
	over := make(chan Report, 1)
	wd := NewWatchdog(tr, Options{
		Bound:    200 * time.Millisecond,
		Progress: func(r Report) { prog <- r },
		Overrun:  func(r Report) { over <- r },
	})
	wd.Arm()
	wd.Arm() // idempotent
	select {
	case r := <-prog:
		if r.Blocking() != "watcher" {
			t.Fatalf("progress blocking = %q", r.Blocking())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no progress report")
	}
	select {
	case r := <-over:
		if r.Blocking() != "watcher" || r.Stage != StageComponents || r.Elapsed < 200*time.Millisecond {
			t.Fatalf("overrun report = %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no overrun report")
	}
	wd.Disarm()
	wd.Disarm() // idempotent
}

func TestWatchdogDisarmBeforeBoundIsSilent(t *testing.T) {
	tr := NewTracker()
	fired := make(chan string, 2)
	wd := NewWatchdog(tr, Options{
		Bound:    150 * time.Millisecond,
		Progress: func(Report) { fired <- "progress" },
		Overrun:  func(Report) { fired <- "overrun" },
	})
	wd.Arm()
	wd.Disarm()
	time.Sleep(300 * time.Millisecond)
	select {
	case f := <-fired:
		t.Fatalf("%s fired after Disarm", f)
	default:
	}
	// Arm after Disarm does nothing.
	wd.Arm()
	time.Sleep(300 * time.Millisecond)
	select {
	case f := <-fired:
		t.Fatalf("%s fired after a post-Disarm Arm", f)
	default:
	}
}

// TestNoForbiddenImports pins the module boundary (CLAUDE.md section 1).
func TestNoForbiddenImports(t *testing.T) {
	forbidden := []string{"database/sql", "net/http", "net", "os", "os/exec", "github.com/fsnotify/fsnotify"}
	fset := token.NewFileSet()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no Go files found; the pin would pass vacuously")
	}
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range af.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if path == bad {
					t.Errorf("%s imports forbidden %q; internal/shutdown must stay pure", f, bad)
				}
			}
		}
	}
}
