package main

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/govern/sidecar"
)

// releasedSidecarFields reads the released governance-sidecar field set the
// sidecar package pins as a fixture (internal/govern/sidecar/testdata).
func releasedSidecarFields(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("../../internal/govern/sidecar/testdata/released-v1-fields.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Fields []string `json:"fields"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, f := range fx.Fields {
		out[f] = true
	}
	return out
}

// assertReleasedReadable fails when the sidecar on disk carries a field a
// released observer's strict decoder does not know (it would refuse the
// whole file after an update rollback).
func assertReleasedReadable(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	known := releasedSidecarFields(t)
	var unknown []string
	for k := range top {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	if len(unknown) != 0 {
		t.Fatalf("the sidecar carries %v, which a released observer refuses: %s", unknown, raw)
	}
}

// TestSidecarWriterRecordsFeatureGrantExceptions (request-grants G8): the
// daemon, the one writer of the governance sidecar, records the feature
// access exceptions its verified grant cache holds for the keys it pins in
// their OWN file beside the sidecar, bound to the sidecar's posture, and
// rewrites it as soon as they change even when the posture did not. The
// sidecar itself stays readable by a released observer throughout, and a
// lapsed exception removes the file.
func TestSidecarWriterRecordsFeatureGrantExceptions(t *testing.T) {
	dir := t.TempDir()
	h, path := sidecarTestHandle(t, dir, `{"schema":2,"pinned":{"guard.enabled":true,"guard.mode":"enforce"}}`, startupSidecar{})
	exPath := sidecar.ExceptionsPath(path)
	f, _ := sidecar.Read(path, time.Now())
	if f == nil {
		t.Fatal("no sidecar")
	}
	if _, err := os.Stat(exPath); !os.IsNotExist(err) {
		t.Fatalf("an exceptions file exists before any grant (stat err=%v)", err)
	}
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	var asked []string
	h.SetFeatureExceptions(func(_ context.Context, keys []string, _ time.Time) map[string]sidecar.Exception {
		asked = keys
		return map[string]sidecar.Exception{"guard.mode": {GrantID: 31, Until: sidecar.FormatTime(until)}}
	})
	h.WriteSidecar(context.Background())
	if err := h.SidecarWriteErr(); err != nil {
		t.Fatal(err)
	}
	assertReleasedReadable(t, path)
	f, _ = sidecar.Read(path, time.Now())
	ex, reason := sidecar.ReadExceptions(exPath, f.EffectiveHash)
	if reason != sidecar.ReasonNone || ex["guard.mode"].GrantID != 31 || ex["guard.mode"].Until != sidecar.FormatTime(until) {
		t.Fatalf("exceptions not written on change: %v (%q)", ex, reason)
	}
	if len(asked) != 2 || asked[0] != "guard.enabled" || asked[1] != "guard.mode" {
		t.Fatalf("asked about %v, want the pinned keys sorted", asked)
	}
	if pins, lifted := sidecar.PinsInForce(f.Pinned, ex, time.Now()); lifted["guard.mode"] != 31 || pins["guard.mode"] != nil || pins["guard.enabled"] != true {
		t.Fatalf("pins in force %v lifted %v", pins, lifted)
	}
	// The grant lapses: the next pass removes the file at once.
	h.SetFeatureExceptions(func(context.Context, []string, time.Time) map[string]sidecar.Exception { return nil })
	h.WriteSidecar(context.Background())
	if _, err := os.Stat(exPath); !os.IsNotExist(err) {
		t.Fatalf("a lapsed exception's file stayed (stat err=%v)", err)
	}
	assertReleasedReadable(t, path)
}
