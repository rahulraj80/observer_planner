package budgetwarnfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestT519AdmissionStopFor pins which published stop a hook process applies.
func TestT519AdmissionStopFor(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 5, 20, 27, 0, 0, time.UTC)
	tomorrow := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		stops    []Stop
		tool     string
		wantRule string
	}{
		{"node-wide stop covers every tool", []Stop{{RuleID: "B-602", ValidUntil: tomorrow}}, "cursor", "B-602"},
		{"expired stop does not apply", []Stop{{RuleID: "B-602", ValidUntil: now}}, "cursor", ""},
		{"zero horizon holds until rewritten", []Stop{{RuleID: "B-625"}}, "cursor", "B-625"},
		{"tool stop matches the tool, case-folded", []Stop{{Tool: "cursor", RuleID: "B-626"}}, " Cursor ", "B-626"},
		{"tool stop never stops another tool", []Stop{{Tool: "cursor", RuleID: "B-626"}}, "codex", ""},
		{"a rule-less row is ignored", []Stop{{Reason: "x"}}, "cursor", ""},
		{"nothing published", nil, "cursor", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := (&Admission{WrittenAt: now.Add(-time.Second), Stops: tc.stops}).StopFor(tc.tool, now)
			if (tc.wantRule != "") != ok || got.RuleID != tc.wantRule {
				t.Fatalf("StopFor = %+v ok=%v, want rule %q", got, ok, tc.wantRule)
			}
		})
	}
	if _, ok := (*Admission)(nil).StopFor("cursor", now); ok {
		t.Fatal("a nil admission stops nothing")
	}
}

// TestP305AdmissionFreshness pins D-359 / review P3-05: a published stop is in
// force only while its publisher keeps it fresh. A B-625 with no ValidUntil
// left behind by a daemon that no longer runs (unenrolled, [org_client] off)
// must not deny every hook forever.
func TestP305AdmissionFreshness(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	stops := []Stop{{RuleID: "B-625", Reason: "r"}}
	cases := []struct {
		name      string
		writtenAt time.Time
		wantStop  bool
	}{
		{"fresh file stops", now.Add(-10 * time.Second), true},
		{"at the bound still stops", now.Add(-AdmissionMaxAge), true},
		{"older than the bound is ignored", now.Add(-AdmissionMaxAge - time.Second), false},
		{"a day-old zero-horizon stop is ignored", now.Add(-24 * time.Hour), false},
		{"no WrittenAt is ignored", time.Time{}, false},
		{"far-future WrittenAt is ignored", now.Add(AdmissionMaxAge + time.Minute), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := &Admission{WrittenAt: tc.writtenAt, Stops: stops}
			if _, ok := a.StopFor("cursor", now); ok != tc.wantStop {
				t.Fatalf("StopFor ok=%v, want %v (stale=%v)", ok, tc.wantStop, a.Stale(now))
			}
		})
	}
	if AdmissionRefresh*3 >= AdmissionMaxAge {
		t.Fatal("the refresh cadence must leave room for missed refreshes inside the bound")
	}
}

// TestT519AdmissionRoundTrip pins the on-disk contract: placement beside the
// DB, the atomic write, a missing file read as "nothing published" and a
// foreign schema refused.
func TestT519AdmissionRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := AdmissionPath(filepath.Join(dir, "observer.db"))
	if filepath.Base(path) != AdmissionFilename || filepath.Dir(path) != dir {
		t.Fatalf("AdmissionPath = %q", path)
	}
	if a, err := ReadAdmission(path); a != nil || err != nil {
		t.Fatalf("missing file = %+v, %v; want nil, nil", a, err)
	}
	want := Admission{WrittenAt: time.Now().UTC().Truncate(time.Second), Stops: []Stop{{RuleID: "B-602", Reason: "r"}}}
	if err := WriteAdmission(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadAdmission(path)
	if err != nil || got == nil || !SameStops(got.Stops, want.Stops) || got.Schema != AdmissionSchema {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	if err := os.WriteFile(path, []byte(`{"schema":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAdmission(path); err == nil {
		t.Fatal("a foreign schema must be refused")
	}
	if AdmissionPath("") != "" {
		t.Fatal("empty db path has no admission file")
	}
}

// TestT519SameStops pins the no-rewrite comparison.
func TestT519SameStops(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	a := []Stop{{RuleID: "B-602", Reason: "r", ValidUntil: at}}
	if !SameStops(a, []Stop{{RuleID: "B-602", Reason: "r", ValidUntil: at.In(time.FixedZone("x", 3600))}}) {
		t.Fatal("the same instant in another zone is the same stop")
	}
	if SameStops(a, []Stop{{RuleID: "B-602", Reason: "r2", ValidUntil: at}}) || SameStops(a, nil) {
		t.Fatal("a different reason or count is a different decision")
	}
}
