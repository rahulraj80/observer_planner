package store

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// TestToolInstallRecordLifecycle walks one record through the whole
// install -> uninstall -> forget lifecycle against a real migrated DB, and
// pins the compare-and-set guards: a second claim, a stale finish and a
// forget of an in-flight row are all refused.
func TestToolInstallRecordLifecycle(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)

	argv := []string{"/home/u/.nvm/bin/npm", "install", "--global", "--prefix", "/home/u/.local", "cline"}
	id, err := s.InsertToolInstall(ctx, ToolInstallRow{
		Tool: "cline-cli", Channel: "npm", Package: "cline", Argv: argv,
		Display: "npm install --global --prefix /home/u/.local cline", Status: "installing", StartedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.LatestToolInstall(ctx, "cline-cli")
	if err != nil || !ok {
		t.Fatalf("LatestToolInstall: ok=%v err=%v", ok, err)
	}
	if got.ID != id || got.Status != "installing" || !reflect.DeepEqual(got.Argv, argv) || !got.StartedAt.Equal(t0) {
		t.Fatalf("unexpected row: %+v", got)
	}

	// Forget is refused while in flight.
	if ok, err := s.ForgetToolInstall(ctx, id, []string{"installed", "install_failed"}, t0); err != nil || ok {
		t.Fatalf("forget in flight: ok=%v err=%v", ok, err)
	}
	// Finish the install.
	if ok, err := s.FinishToolInstall(ctx, id, "installing", ToolInstallFinish{Status: "installed", ExitCode: 0, ResolvedBin: "/home/u/.local/bin/cline", At: t0.Add(time.Minute)}); err != nil || !ok {
		t.Fatalf("finish: ok=%v err=%v", ok, err)
	}
	// A stale second finish loses the CAS.
	if ok, _ := s.FinishToolInstall(ctx, id, "installing", ToolInstallFinish{Status: "install_failed", ExitCode: 1, At: t0}); ok {
		t.Fatal("second finish must lose the CAS")
	}
	got, _, _ = s.LatestToolInstall(ctx, "cline-cli")
	if got.Status != "installed" || got.ExitCode == nil || *got.ExitCode != 0 || got.ResolvedBin != "/home/u/.local/bin/cline" || got.FinishedAt == nil {
		t.Fatalf("after finish: %+v", got)
	}

	// Claim the uninstall; a second claim is refused.
	uargv := []string{"/home/u/.nvm/bin/npm", "uninstall", "--global", "--prefix", "/home/u/.local", "cline"}
	from := []string{"installed", "uninstall_failed"}
	prev, ok, err := s.ClaimToolUninstall(ctx, id, from, "uninstalling", uargv, "npm uninstall --global --prefix /home/u/.local cline", t0.Add(2*time.Minute))
	if err != nil || !ok || prev != "installed" {
		t.Fatalf("claim: prev=%q ok=%v err=%v", prev, ok, err)
	}
	if _, ok, _ := s.ClaimToolUninstall(ctx, id, from, "uninstalling", uargv, "x", t0); ok {
		t.Fatal("double claim must be refused")
	}
	// Spawn failed: revert, then claim again.
	if ok, err := s.RevertToolUninstall(ctx, id, "uninstalling", prev); err != nil || !ok {
		t.Fatalf("revert: ok=%v err=%v", ok, err)
	}
	if _, ok, _ := s.ClaimToolUninstall(ctx, id, from, "uninstalling", uargv, "npm uninstall --global --prefix /home/u/.local cline", t0.Add(3*time.Minute)); !ok {
		t.Fatal("re-claim after revert must succeed")
	}
	if ok, err := s.FinishToolUninstall(ctx, id, "uninstalling", ToolInstallFinish{Status: "uninstalled", ExitCode: 0, At: t0.Add(4 * time.Minute)}); err != nil || !ok {
		t.Fatalf("finish uninstall: ok=%v err=%v", ok, err)
	}
	got, _, _ = s.LatestToolInstall(ctx, "cline-cli")
	if got.Status != "uninstalled" || !reflect.DeepEqual(got.UninstallArgv, uargv) || got.UninstallExitCode == nil || got.UninstallFinishedAt == nil || got.ResolvedBin != "" {
		t.Fatalf("after uninstall: %+v", got)
	}

	// A second install of another tool; the list is latest-per-tool.
	id2, err := s.InsertToolInstall(ctx, ToolInstallRow{Tool: "claude-code", Channel: "script", Argv: []string{"bash", "-lc", "curl … | bash"}, Display: "curl … | bash", Status: "installing", StartedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.SetToolInstallStatus(ctx, id2, "installing", "install_interrupted"); !ok {
		t.Fatal("orphan sweep CAS failed")
	}
	list, err := s.ListLatestToolInstalls(ctx)
	if err != nil || len(list) != 2 || list[0].ID != id2 || list[1].ID != id {
		t.Fatalf("list = %+v err=%v", list, err)
	}

	// Forget hides the record but keeps the row.
	if ok, err := s.ForgetToolInstall(ctx, id, []string{"uninstalled"}, t0); err != nil || !ok {
		t.Fatalf("forget: ok=%v err=%v", ok, err)
	}
	if _, ok, _ := s.LatestToolInstall(ctx, "cline-cli"); ok {
		t.Fatal("forgotten record must not be latest")
	}
	if _, ok, _ := s.GetToolInstall(ctx, id); !ok {
		t.Fatal("forgotten row must still exist")
	}
	if ok, _ := s.ForgetToolInstall(ctx, id, []string{"uninstalled"}, t0); ok {
		t.Fatal("double forget must be refused")
	}
	if _, ok, _ := s.ClaimToolUninstall(ctx, id, []string{"uninstalled"}, "uninstalling", uargv, "x", t0); ok {
		t.Fatal("a forgotten record can never be claimed")
	}
}

// TestToolInstallClaimAndForgetRefuseASupersededRecord (Sol A3-08): the
// ledger checks "is this the latest not-forgotten record?" and then claims or
// forgets it in a separate step, so a new install can land between the two.
// The store's claim and forget must themselves refuse a record that a newer
// not-forgotten record of the same tool supersedes, or an uninstall runs
// against machine state the newer install now owns.
func TestToolInstallClaimAndForgetRefuseASupersededRecord(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	insert := func(tool, status string) int64 {
		t.Helper()
		id, err := s.InsertToolInstall(ctx, ToolInstallRow{
			Tool: tool, Channel: "npm", Package: "cline", Argv: []string{"npm", "install", "cline"},
			Display: "npm install cline", Status: status, StartedAt: t0,
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	old := insert("cline-cli", "installed")
	// The window: a newer install of the same tool lands after the
	// operator's latest-record check.
	newer := insert("cline-cli", "installing")

	from := []string{"installed", "uninstall_failed"}
	if _, ok, err := s.ClaimToolUninstall(ctx, old, from, "uninstalling", []string{"npm", "uninstall", "cline"}, "npm uninstall cline", t0); err != nil || ok {
		t.Fatalf("claim of a superseded record: ok=%v err=%v, want refused", ok, err)
	}
	if ok, err := s.ForgetToolInstall(ctx, old, []string{"installed"}, t0); err != nil || ok {
		t.Fatalf("forget of a superseded record: ok=%v err=%v, want refused", ok, err)
	}
	got, _, _ := s.GetToolInstall(ctx, old)
	if got.Status != "installed" {
		t.Fatalf("superseded record changed: %+v", got)
	}

	// Another tool's newer record does not supersede it, and once the newer
	// same-tool record is forgotten the old one is the latest again.
	_ = insert("claude-code", "installing")
	if ok, err := s.ForgetToolInstall(ctx, newer, []string{"installing"}, t0); err != nil || !ok {
		t.Fatalf("forget newer: ok=%v err=%v", ok, err)
	}
	if _, ok, err := s.ClaimToolUninstall(ctx, old, from, "uninstalling", []string{"npm", "uninstall", "cline"}, "npm uninstall cline", t0); err != nil || !ok {
		t.Fatalf("claim of the latest record again: ok=%v err=%v, want claimed", ok, err)
	}
}
