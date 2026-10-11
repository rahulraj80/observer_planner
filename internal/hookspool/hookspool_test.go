package hookspool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
)

func rec(sid string) Record {
	return FromEvent(models.ToolEvent{
		Tool: "qoder", SourceFile: "qoder:hook", SourceEventID: sid + ":session_end", SessionID: sid,
		ActionType: models.ActionSessionEnd, Target: "session_ended", Success: true,
		Timestamp: time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC),
		Metadata:  &models.ActionMetadata{PermissionMode: "default"},
	})
}

func TestPath(t *testing.T) {
	if Path("") != "" {
		t.Fatal("empty config path must give an empty spool path")
	}
	if got := Path(filepath.Join("a", "config.toml")); got != filepath.Join("a", Filename) {
		t.Fatalf("Path = %q", got)
	}
}

func TestAppendValidates(t *testing.T) {
	p := filepath.Join(t.TempDir(), Filename)
	for name, r := range map[string]Record{
		"no session": {Tool: "t", ActionType: "a"},
		"no action":  {Tool: "t", SessionID: "s"},
		"no tool":    {SessionID: "s", ActionType: "a"},
	} {
		if err := Append(p, r); err == nil {
			t.Errorf("%s: Append accepted an incomplete record", name)
		}
	}
	if err := Append("", rec("s")); err == nil {
		t.Error("Append accepted an empty path")
	}
	big := rec("s")
	big.ToolOutput = strings.Repeat("x", MaxRecordBytes)
	if err := Append(p, big); err == nil {
		t.Error("Append accepted an oversized record")
	}
}

// TestRoundTrip is the two-phase protocol: append, set aside, wait out the
// grace, read, done.
func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, Filename)
	for _, sid := range []string{"s1", "s2"} {
		if err := Append(p, rec(sid)); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if ok, err := SetAside(p, now); err != nil || !ok {
		t.Fatalf("SetAside = %v, %v", ok, err)
	}
	if ok, _ := SetAside(p, now); ok {
		t.Fatal("a second SetAside with no new appends must be a no-op")
	}
	if r, _ := Ready(p, now.Add(Grace/2)); len(r) != 0 {
		t.Fatalf("an aside file inside the grace window was returned: %d", len(r))
	}
	ready, err := Ready(p, now.Add(Grace+time.Millisecond))
	if err != nil || len(ready) != 1 {
		t.Fatalf("Ready = %d, %v", len(ready), err)
	}
	a := ready[0]
	if len(a.Records) != 2 || a.Dropped != 0 {
		t.Fatalf("records %d dropped %d", len(a.Records), a.Dropped)
	}
	ev := a.Records[0].Event()
	if ev.SessionID != "s1" || ev.ActionType != models.ActionSessionEnd || ev.Metadata == nil || ev.Metadata.PermissionMode != "default" || !ev.Success {
		t.Fatalf("event did not round-trip: %+v", ev)
	}
	if err := Done(a); err != nil {
		t.Fatal(err)
	}
	if err := Done(a); err != nil {
		t.Fatalf("Done on a removed file must be a no-op: %v", err)
	}
	if r, _ := Ready(p, now.Add(time.Hour)); len(r) != 0 {
		t.Fatal("a done aside file was returned again")
	}
}

func TestReadyDropsBadLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, Filename)
	if err := Append(p, rec("ok")); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("not json\n{\"v\":99,\"tool\":\"t\",\"session_id\":\"s\",\"action_type\":\"a\"}\n\n")
	_ = f.Close()
	now := time.Now()
	if _, err := SetAside(p, now); err != nil {
		t.Fatal(err)
	}
	ready, _ := Ready(p, now.Add(time.Minute))
	if len(ready) != 1 || len(ready[0].Records) != 1 || ready[0].Dropped != 2 {
		t.Fatalf("ready = %+v", ready)
	}
}

func TestReadyOrdersOldestFirst(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, Filename)
	base := time.Now()
	for i, sid := range []string{"first", "second"} {
		if err := Append(p, rec(sid)); err != nil {
			t.Fatal(err)
		}
		if _, err := SetAside(p, base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	ready, _ := Ready(p, base.Add(time.Minute))
	if len(ready) != 2 || ready[0].Records[0].SessionID != "first" {
		t.Fatalf("order wrong: %+v", ready)
	}
}
