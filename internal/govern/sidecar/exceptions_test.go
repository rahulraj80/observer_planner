package sidecar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestExceptionsPathIsBesideTheSidecar: one derivation, used by the writer
// (the daemon), every reader (config.Load) and unenrol.
func TestExceptionsPathIsBesideTheSidecar(t *testing.T) {
	p := filepath.Join("a", "b", "governance-effective.json")
	if got, want := ExceptionsPath(p), filepath.Join("a", "b", ExceptionsFilename); got != want {
		t.Fatalf("ExceptionsPath = %q, want %q", got, want)
	}
	if ExceptionsPath("") != "" {
		t.Fatal("no sidecar path must mean no exceptions path")
	}
}

// TestReadExceptionsFailureTable: every failure is no exceptions (every pin
// stays) with a named reason, never an error; and the file is honoured only
// for the posture it was written beside.
func TestReadExceptionsFailureTable(t *testing.T) {
	live := `{"schema":1,"effective_hash":"h1","exceptions":{"guard.mode":{"grant_id":31,"until":"2026-10-04T18:00:00Z"}}}`
	cases := []struct {
		name   string
		body   string
		hash   string
		absent bool
		dir    bool
		want   string
	}{
		{name: "absent", absent: true, hash: "h1", want: ReasonAbsent},
		{name: "directory", dir: true, hash: "h1", want: ReasonUnreadable},
		{name: "oversize", body: `{"schema":1,"effective_hash":"` + strings.Repeat("x", MaxBytes) + `"}`, hash: "h1", want: ReasonOversize},
		{name: "malformed", body: `{"schema":1,`, hash: "h1", want: ReasonMalformed},
		{name: "unknown field", body: `{"schema":1,"effective_hash":"h1","exceptions":{},"surprise":1}`, hash: "h1", want: ReasonMalformed},
		{name: "trailing bytes", body: live + ` {}`, hash: "h1", want: ReasonMalformed},
		{name: "schema too new", body: `{"schema":2,"effective_hash":"h1","exceptions":{}}`, hash: "h1", want: ReasonSchemaTooNew},
		{name: "schema zero", body: `{"effective_hash":"h1","exceptions":{}}`, hash: "h1", want: ReasonSchemaTooNew},
		{name: "another posture", body: live, hash: "h2", want: ReasonStale},
		{name: "sidecar without a hash", body: live, hash: "", want: ReasonStale},
		{name: "file without a hash", body: `{"schema":1,"effective_hash":"","exceptions":{"guard.mode":{"grant_id":31,"until":"2026-10-04T18:00:00Z"}}}`, hash: "", want: ReasonStale},
		{name: "live", body: live, hash: "h1", want: ReasonNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ExceptionsFilename)
			switch {
			case tc.absent:
			case tc.dir:
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, reason := ReadExceptions(path, tc.hash)
			if reason != tc.want {
				t.Fatalf("reason = %q, want %q", reason, tc.want)
			}
			if (got != nil) != (tc.want == ReasonNone) {
				t.Fatalf("exceptions %v for reason %q", got, reason)
			}
		})
	}
}

// TestExceptionsRoundTrip: what the daemon encodes is what a reader decodes,
// canonically.
func TestExceptionsRoundTrip(t *testing.T) {
	in := ExceptionsFile{EffectiveHash: "h1", Exceptions: map[string]Exception{
		"guard.mode": {GrantID: 31, Until: "2026-10-04T18:00:00Z"},
		"a.key":      {GrantID: 7, Until: "2026-10-04T19:00:00Z"},
	}}
	a, err := EncodeExceptions(in)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := EncodeExceptions(in)
	if string(a) != string(b) {
		t.Fatal("encoding is not canonical")
	}
	out, err := DecodeExceptions(a)
	if err != nil {
		t.Fatal(err)
	}
	if out.Schema != ExceptionsMaxSchema || out.Exceptions["guard.mode"].GrantID != 31 || out.EffectiveHash != "h1" {
		t.Fatalf("round trip lost data: %+v", out)
	}
}

// TestPinsInForce: an exception removes exactly its pin until its instant;
// it never sets a value, and an unreadable or grant-less one lifts nothing.
func TestPinsInForce(t *testing.T) {
	until := time.Date(2026, 10, 4, 18, 0, 0, 0, time.UTC)
	pinned := map[string]any{"guard.enabled": true, "guard.mode": "enforce", "routing.enabled": false}
	ex := map[string]Exception{
		"guard.mode":      {GrantID: 31, Until: FormatTime(until)},
		"routing.enabled": {GrantID: 0, Until: FormatTime(until)},
		"unpinned.key":    {GrantID: 33, Until: FormatTime(until)},
		"guard.enabled":   {GrantID: 34, Until: "soon"},
	}
	pins, lifted := PinsInForce(pinned, ex, until.Add(-time.Minute))
	if len(lifted) != 1 || lifted["guard.mode"] != 31 {
		t.Fatalf("lifted = %v, want only guard.mode", lifted)
	}
	if _, ok := pins["guard.mode"]; ok || len(pins) != 2 || pins["guard.enabled"] != true {
		t.Fatalf("pins in force = %v", pins)
	}
	if _, ok := pins["unpinned.key"]; ok {
		t.Fatal("an exception set a value")
	}
	if len(pinned) != 3 {
		t.Fatal("PinsInForce modified its input")
	}
	if _, lifted := PinsInForce(pinned, ex, until); len(lifted) != 0 {
		t.Fatalf("at the until instant the pin is back, lifted = %v", lifted)
	}
}
