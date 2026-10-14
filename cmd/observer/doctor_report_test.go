package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
)

// TestBuildDoctorReportPerTool pins the one assembly `observer doctor <tool>`
// and the dashboard's per-tool "Run checks" (T-421 item 3) share: an adapter
// scope gives the focused view (the adapter's own check first, then control
// coverage) with the whole-database integrity probe deferred; a non-adapter
// scope filters; an unmatched scope errors; the dashboard seam runs the same
// builder.
func TestBuildDoctorReportPerTool(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Observer.DBPath = filepath.Join(dir, "o.db")
	database, err := dbtemplate.Open(ctx, db.Options{Path: cfg.Observer.DBPath})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	in := doctorReportInput{Config: cfg, DB: database, Binary: filepath.Join(dir, "observer"), ConfigPath: filepath.Join(dir, "config.toml"), HomeDir: dir}

	in.Scope = "cursor"
	rep, err := buildDoctorReport(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Checks) == 0 || rep.Checks[0].Name != "cursor" {
		t.Fatalf("per-adapter view does not lead with the adapter check: %+v", rep.Checks)
	}
	names := []string{}
	for _, c := range rep.Checks {
		names = append(names, c.Name)
		if c.Name == "db.integrity" && !strings.HasPrefix(c.Message, "skipped") {
			t.Fatalf("a per-tool doctor ran the whole-database probe: %q", c.Message)
		}
	}
	if !strings.Contains(strings.Join(names, ","), "cursor.control-coverage") {
		t.Fatalf("checks = %v, want the control coverage line", names)
	}

	via, err := dashboardToolDoctor(in.ConfigPath)(ctx, cfg, database, "cursor")
	if err != nil || len(via.Checks) == 0 || via.Checks[0].Name != "cursor" {
		t.Fatalf("dashboard seam = %+v %v", via.Checks, err)
	}

	in.Scope = "db"
	rep, err = buildDoctorReport(ctx, in)
	if err != nil || rep.Matched() == 0 {
		t.Fatalf("db scope: %+v %v", rep, err)
	}
	for _, c := range rep.Checks {
		if !strings.Contains(c.Name, "db") {
			t.Fatalf("db scope kept %q", c.Name)
		}
	}

	in.Scope = "zz-no-such-thing"
	if _, err := buildDoctorReport(ctx, in); err == nil {
		t.Fatal("an unmatched scope must error")
	}
}
