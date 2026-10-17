//go:build !sbo_omit_archive

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/archive"
)

// TestArchiveReclaimCleanup pins T-493 end to end: after a real reclaim the
// leftover is listed (and kept) without --delete, deleted with --delete once
// the database in place passes quick_check, and the database stays intact.
func TestArchiveReclaimCleanup(t *testing.T) {
	dir := t.TempDir()
	dbPath, _ := reclaimTestDB(t, dir)
	cfgPath := writeReclaimConfig(t, dir, dbPath)
	if out, err := runReclaim(t, "reclaim", "--config", cfgPath, "--run", "--force", "--json"); err != nil {
		t.Fatalf("reclaim --run: %v\n%s", err, out)
	}

	out, err := runReclaim(t, "reclaim-cleanup", "--config", cfgPath, "--json")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	var rep cleanupReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if len(rep.Leftovers) != 1 || rep.Bytes <= 0 || rep.Plan != nil || len(rep.Deleted) != 0 {
		t.Fatalf("list report = %+v", rep)
	}
	backup := filepath.Join(dir, rep.Leftovers[0].Files[0].Name)
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("listing deleted the leftover: %v", err)
	}

	out, err = runReclaim(t, "reclaim-cleanup", "--config", cfgPath, "--delete", "--json")
	if err != nil {
		t.Fatalf("delete: %v\n%s", err, out)
	}
	rep = cleanupReport{}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if rep.Plan == nil || !rep.Plan.OK() || len(rep.Deleted) == 0 || rep.FreedBytes != rep.Bytes {
		t.Fatalf("delete report = %+v", rep)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("leftover still on disk: %v", err)
	}
	if fileSizeOrZero(dbPath) == 0 {
		t.Fatal("the live database was touched")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".pre-reclaim-") {
			t.Fatalf("leftover file survived: %s", e.Name())
		}
	}
}

// TestArchiveReclaimCleanupRefuses: the refusal rows reach the command, and a
// refusal deletes nothing.
func TestArchiveReclaimCleanupRefuses(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, dbPath string)
		want  archive.CleanupDecision
	}{
		{"reclaim in progress", func(t *testing.T, dbPath string) {
			if err := os.WriteFile(dbPath+".reclaim-20261007T101010Z", []byte("partial"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, archive.CleanupInProgress},
		{"no database in place", func(t *testing.T, dbPath string) {
			if err := os.Remove(dbPath); err != nil {
				t.Fatal(err)
			}
		}, archive.CleanupLiveMissing},
		{"database in place is not a database", func(t *testing.T, dbPath string) {
			if err := os.WriteFile(dbPath, []byte("this is not an sqlite file at all, just text"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, archive.CleanupLiveUnhealthy},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			dbPath := filepath.Join(dir, "observer.db")
			cfgPath := writeReclaimConfig(t, dir, dbPath)
			leftover := dbPath + ".pre-reclaim-20260917T043021Z"
			if err := os.WriteFile(leftover, []byte("the only copy, maybe"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dbPath, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			c.setup(t, dbPath)
			out, err := runReclaim(t, "reclaim-cleanup", "--config", cfgPath, "--delete", "--json")
			if err == nil {
				t.Fatalf("a refused cleanup must exit non-zero:\n%s", out)
			}
			var rep cleanupReport
			if derr := json.Unmarshal([]byte(out[:strings.LastIndex(out, "}")+1]), &rep); derr != nil {
				t.Fatalf("decode: %v\n%s", derr, out)
			}
			if rep.Plan == nil || rep.Plan.Decision != c.want {
				t.Fatalf("plan = %+v, want %s", rep.Plan, c.want)
			}
			if _, serr := os.Stat(leftover); serr != nil {
				t.Fatalf("a refused cleanup deleted the leftover: %v", serr)
			}
		})
	}
}
