package guardposture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPostureRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	path := PosturePath(cfg)
	if filepath.Dir(path) != dir || filepath.Base(path) != PostureFilename {
		t.Fatalf("PosturePath = %q", path)
	}
	got, err := Read(path)
	if err != nil || got != nil {
		t.Fatalf("missing file: got %+v, %v; want nil, nil", got, err)
	}
	want := Posture{Enforced: true, Source: SourceOrg, Generation: "12", GateCaps: []string{"tool_gate", "prompt_gate"}, WrittenAt: time.Unix(100, 0).UTC()}
	if _, err := Write(path, want, testKey); err != nil {
		t.Fatal(err)
	}
	got, err = Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enforced || got.Source != SourceOrg || got.Generation != "12" || len(got.GateCaps) != 2 || got.Schema != Schema {
		t.Fatalf("round trip = %+v", got)
	}
	if st, err := os.Stat(path); err == nil && st.Mode().Perm() != 0o600 && os.PathSeparator == '/' {
		t.Errorf("posture mode = %v, want 0600", st.Mode().Perm())
	}
}

func TestFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    *Posture
		cap  string
		want bool
	}{
		{"nil posture", nil, "tool_gate", false},
		{"not enforced", &Posture{Enforced: false, GateCaps: []string{"tool_gate"}}, "tool_gate", false},
		{"enforced listed", &Posture{Enforced: true, GateCaps: []string{"tool_gate"}}, "tool_gate", true},
		{"enforced not listed", &Posture{Enforced: true, GateCaps: []string{"tool_gate"}}, "prompt_gate", false},
		{"enforced lifecycle", &Posture{Enforced: true, GateCaps: []string{"tool_gate", "prompt_gate"}}, "session_lifecycle", false},
	} {
		if got := tc.p.FailsClosed(tc.cap); got != tc.want {
			t.Errorf("%s: FailsClosed(%s) = %v, want %v", tc.name, tc.cap, got, tc.want)
		}
	}
}

// TestPostureStale is the T-050 freshness table.
func TestPostureStale(t *testing.T) {
	now := time.Unix(10_000, 0)
	for _, tc := range []struct {
		name      string
		p         *Posture
		cfgMod    time.Time
		wantStale bool
	}{
		{"nil", nil, time.Time{}, true},
		{"fresh, config older", &Posture{WrittenAt: now.Add(-time.Minute)}, now.Add(-time.Hour), false},
		{"fresh, config unknown", &Posture{WrittenAt: now.Add(-time.Minute)}, time.Time{}, false},
		{"at the window edge", &Posture{WrittenAt: now.Add(-MaxAge)}, time.Time{}, false},
		{"past the window", &Posture{WrittenAt: now.Add(-MaxAge - time.Second)}, time.Time{}, true},
		{"config changed after", &Posture{WrittenAt: now.Add(-time.Minute)}, now.Add(-30 * time.Second), true},
		{"small future skew", &Posture{WrittenAt: now.Add(30 * time.Second)}, time.Time{}, false},
		{"far future", &Posture{WrittenAt: now.Add(time.Hour)}, time.Time{}, true},
	} {
		if got := tc.p.Stale(now, tc.cfgMod); (got != "") != tc.wantStale {
			t.Errorf("%s: Stale = %q, want stale=%v", tc.name, got, tc.wantStale)
		}
	}
}

func TestEncodeDropsCapsWhenNotEnforced(t *testing.T) {
	data, err := Encode(Posture{Enforced: false, GateCaps: []string{"tool_gate"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "tool_gate") {
		t.Errorf("a non-enforced posture carried gate caps: %s", data)
	}
}

func TestReadRejectsForeignSchemaAndGarbage(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"garbage": "not json",
		"schema":  `{"schema":99,"enforced":true}`,
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := Read(p); err == nil || got != nil {
			t.Errorf("%s: Read = %+v, %v; want an error", name, got, err)
		}
	}
}

// TestAppendDrainCommit: concurrent appenders, one drain, commit; a drain
// whose commit never ran is processed again before a new rename.
func TestAppendDrainCommit(t *testing.T) {
	dir := t.TempDir()
	spool := SpoolPath(filepath.Join(dir, "config.toml"))
	var wg sync.WaitGroup
	const n = 40
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Append(spool, Record{Tool: "claude-code", Event: "PreToolUse", Cap: "tool_gate", Kind: KindTimeout}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	b, err := Drain(spool, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Records) != n || b.Dropped != 0 {
		t.Fatalf("drained %d records (%d dropped), want %d", len(b.Records), b.Dropped, n)
	}
	// A hook appending after the rename writes a fresh spool.
	if err := Append(spool, Record{Tool: "qoder", Event: "UserPromptSubmit", Cap: "prompt_gate", Kind: KindCrash}); err != nil {
		t.Fatal(err)
	}
	// Commit never ran: the next drain re-processes the aside file first.
	again, err := Drain(spool, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Records) != n {
		t.Fatalf("uncommitted drain re-read %d records, want %d", len(again.Records), n)
	}
	if err := again.Commit(); err != nil {
		t.Fatal(err)
	}
	// Fully read but still inside its quiet window: nothing to drain yet,
	// and the live spool waits.
	if early, err := Drain(spool, 0); err != nil || early != nil {
		t.Fatalf("drain inside the quiet window = %+v, %v; want nil, nil", early, err)
	}
	advanceDrainClock(t, SpoolQuiesce)
	next, err := Drain(spool, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || len(next.Records) != 1 || next.Records[0].Kind != KindCrash {
		t.Fatalf("post-commit drain = %+v", next)
	}
	if err := next.Commit(); err != nil {
		t.Fatal(err)
	}
	empty, err := Drain(spool, 0)
	if err != nil || empty != nil {
		t.Fatalf("empty spool drain = %+v, %v; want nil, nil", empty, err)
	}
}

// advanceDrainClock moves the drain's clock forward by d for the rest of
// the test.
func advanceDrainClock(t *testing.T, d time.Duration) {
	t.Helper()
	prev := nowFn
	base := prev()
	nowFn = func() time.Time { return base.Add(d) }
	t.Cleanup(func() { nowFn = prev })
}

// TestDrainKeepsStragglerWrittenAfterRead reproduces T-051: a hook opens the
// spool BEFORE the drain seals it, and writes AFTER the drain read the
// sealed file but before the drain committed. On POSIX the write lands in
// the sealed inode. The old drain removed that file on Commit and lost the
// line; now the offset is kept and the next drain returns it.
func TestDrainKeepsStragglerWrittenAfterRead(t *testing.T) {
	dir := t.TempDir()
	spool := filepath.Join(dir, SpoolFilename)
	if err := Append(spool, Record{Tool: "claude-code", Event: "PreToolUse", Cap: "tool_gate", Kind: KindTimeout}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX interleaving: Windows refuses to rename an open file, so the drain retries on its next pass instead")
	}
	// The straggling hook: opened before the seal, like Append's OpenFile.
	straggler, err := os.OpenFile(spool, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Drain(spool, 0)
	if err != nil || b == nil || len(b.Records) != 1 {
		t.Fatalf("first drain = %+v, %v", b, err)
	}
	line := `{"tool":"cursor","event":"beforeShellExecution","cap":"tool_gate","kind":"crash","at":"2026-10-03T00:00:00Z"}` + "\n"
	if _, err := straggler.WriteString(line); err != nil {
		t.Fatal(err)
	}
	_ = straggler.Close()
	if err := b.Commit(); err != nil {
		t.Fatal(err)
	}
	// The straggler line is read on the next pass, inside the quiet window.
	next, err := Drain(spool, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || len(next.Records) != 1 || next.Records[0].Tool != "cursor" {
		t.Fatalf("the straggler line was lost: %+v", next)
	}
	if err := next.Commit(); err != nil {
		t.Fatal(err)
	}
	aside := spool + drainingSuffix
	if _, err := os.Stat(aside); err != nil {
		t.Fatalf("the sealed file was removed inside its quiet window: %v", err)
	}
	// After the quiet window a fully read seal is retired.
	advanceDrainClock(t, SpoolQuiesce)
	if again, err := Drain(spool, 0); err != nil || again != nil {
		t.Fatalf("drain after the quiet window = %+v, %v; want nil, nil", again, err)
	}
	for _, p := range []string{aside, aside + drainStateSuffix} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s outlived its quiet window: %v", filepath.Base(p), err)
		}
	}
}

// TestDrainLeavesUnterminatedTail: a partial line at the end of the sealed
// file (a straggler mid-write) is left for a later pass until the seal is
// quiet.
func TestDrainLeavesUnterminatedTail(t *testing.T) {
	dir := t.TempDir()
	spool := filepath.Join(dir, SpoolFilename)
	if err := Append(spool, Record{Tool: "qoder", Kind: KindCrash}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(spool, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"tool":"codex","kind":"timeout"`)
	_ = f.Close()
	b, err := Drain(spool, 0)
	if err != nil || b == nil || len(b.Records) != 1 || !b.More {
		t.Fatalf("drain = %+v, %v; want 1 record and the tail kept", b, err)
	}
	if err := b.Commit(); err != nil {
		t.Fatal(err)
	}
	if again, err := Drain(spool, 0); err != nil || again != nil {
		t.Fatalf("an unterminated tail was consumed early: %+v, %v", again, err)
	}
	advanceDrainClock(t, SpoolQuiesce)
	last, err := Drain(spool, 0)
	if err != nil || last == nil || last.Dropped != 1 {
		t.Fatalf("quiet drain = %+v, %v; want the tail consumed as one dropped line", last, err)
	}
	if err := last.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(spool + drainingSuffix); !os.IsNotExist(err) {
		t.Errorf("the sealed file was not retired: %v", err)
	}
}

func TestAppendClipsAndDrainCapsAndCountsGarbage(t *testing.T) {
	dir := t.TempDir()
	spool := filepath.Join(dir, SpoolFilename)
	if err := Append(spool, Record{Tool: strings.Repeat("x", 10000), Kind: KindConfig}); err != nil {
		t.Fatalf("an oversized field must be clipped, not refused: %v", err)
	}
	f, err := os.OpenFile(spool, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("garbage line\n{\"tool\":\"t\"}\n")
	_ = f.Close()
	b, err := Drain(spool, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Records) != 1 || len(b.Records[0].Tool) != fieldCap || b.Dropped != 2 {
		t.Fatalf("records=%d tool len=%d dropped=%d", len(b.Records), len(b.Records[0].Tool), b.Dropped)
	}
	_ = b.Commit()
	advanceDrainClock(t, SpoolQuiesce)
	for i := 0; i < 50; i++ {
		_ = Append(spool, Record{Tool: "t", Kind: KindTimeout})
	}
	// Sol A2-07: a capped drain reads a chunk of whole lines and keeps the
	// rest; draining chunk by chunk recovers every record, none dropped.
	total, chunks := 0, 0
	for {
		capped, err := Drain(spool, 300)
		if err != nil {
			t.Fatal(err)
		}
		if capped == nil {
			break
		}
		chunks++
		if capped.Dropped != 0 {
			t.Fatalf("chunk %d dropped %d well-formed records", chunks, capped.Dropped)
		}
		total += len(capped.Records)
		if err := capped.Commit(); err != nil {
			t.Fatal(err)
		}
		if chunks > 100 {
			t.Fatal("the drain never finished")
		}
	}
	if total != 50 || chunks < 2 {
		t.Fatalf("drained %d records in %d chunks, want all 50 over several chunks", total, chunks)
	}
}

// TestDrainKeepsTailAcrossTheCap pins Sol A2-07 at the edges: a cap that
// lands in the middle of a record never splits it, an uncommitted chunk is
// re-read whole, an oversize garbage line is counted once and skipped,
// and a spool appended to between chunks is drained too.
func TestDrainKeepsTailAcrossTheCap(t *testing.T) {
	dir := t.TempDir()
	spool := filepath.Join(dir, SpoolFilename)
	for i := 0; i < 3; i++ {
		if err := Append(spool, Record{Tool: "claude-code", Event: "pre-tool", Kind: KindTimeout}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(spool)
	if err != nil {
		t.Fatal(err)
	}
	lineLen := int64(strings.IndexByte(string(data), '\n') + 1)

	// Cap inside the first record: that record is taken whole, the rest kept.
	b, err := Drain(spool, lineLen/2)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Records) != 1 || !b.More || b.Dropped != 0 {
		t.Fatalf("mid-record cap: records %d more %v dropped %d", len(b.Records), b.More, b.Dropped)
	}
	// Not committed: the same chunk comes back.
	again, err := Drain(spool, lineLen/2)
	if err != nil || len(again.Records) != 1 {
		t.Fatalf("uncommitted chunk re-read: %+v %v", again, err)
	}
	if err := again.Commit(); err != nil {
		t.Fatal(err)
	}

	// An oversize garbage line between the remaining records, and a new
	// append to the live spool.
	aside := spool + drainingSuffix
	f, err := os.OpenFile(aside, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(strings.Repeat("x", 3*MaxRecordBytes) + "\n")
	_ = f.Close()
	if err := Append(spool, Record{Tool: "qoder", Kind: KindCrash}); err != nil {
		t.Fatal(err)
	}

	var got []Record
	dropped := 0
	advanceDrainClock(t, SpoolQuiesce)
	for i := 0; i < 20; i++ {
		c, err := Drain(spool, 0)
		if err != nil {
			t.Fatal(err)
		}
		if c == nil {
			break
		}
		got = append(got, c.Records...)
		dropped += c.Dropped
		if err := c.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 3 || dropped != 1 {
		t.Fatalf("remaining drain: %d records, %d dropped; want 3 (2 kept + 1 new) and 1", len(got), dropped)
	}
	advanceDrainClock(t, SpoolQuiesce)
	if c, err := Drain(spool, 0); err != nil || c != nil {
		t.Fatalf("retiring drain = %+v, %v", c, err)
	}
	if _, err := os.Stat(aside); !os.IsNotExist(err) {
		t.Errorf("the aside file outlived a full drain: %v", err)
	}
}

// TestNoForbiddenImports pins the package to stdlib + internal/fsatomic: it
// is read on the hook path.
func TestNoForbiddenImports(t *testing.T) {
	const modulePrefix = "github.com/marmutapp/superbased-observer/"
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, modulePrefix) && p != modulePrefix+"internal/fsatomic" {
				t.Errorf("%s imports %q: guardposture is stdlib + fsatomic only", f, p)
			}
			if !strings.HasPrefix(p, modulePrefix) && strings.Contains(p, ".") {
				t.Errorf("%s imports third-party %q", f, p)
			}
		}
	}
}
