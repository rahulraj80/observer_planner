package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/platform/crossmount"
	"github.com/marmutapp/superbased-observer/internal/proxyroute"
)

// TestWireAIClients_WindowsOnlyWritesOnlyTheWindowsSide pins the option the
// dashboard's Windows-home control rides (T-421 item 4): the same
// `--codex --windows-codex-home` selection, kept to the codex-windows target,
// writes the Windows-side route and leaves the native codex config alone.
func TestWireAIClients_WindowsOnlyWritesOnlyTheWindowsSide(t *testing.T) {
	requireComponents(t, "integration.proxy-route", "integration.mcp-server")
	requireActiveAdapters(t, "codex")
	requireAdapter(t, "codex")
	defer proxyroute.SetWSLForTest(true)()
	home := interactiveHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	winHome := filepath.Join(home, "mnt", "c", "Users", "tester")
	if err := os.MkdirAll(filepath.Join(winHome, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	lines, _, _, _, err := wireAIClients(WireAIClientsOptions{
		ProxyPort:        18820,
		HomeDir:          home,
		OnlyCodex:        true,
		WindowsCodexHome: winHome,
		WindowsOnly:      true,
		SkipExtension:    true,
	})
	if err != nil {
		t.Fatalf("wireAIClients: %v", err)
	}
	text := strings.Join(lines, "\n")
	if _, err := os.Stat(filepath.Join(winHome, ".codex", "config.toml")); err != nil {
		t.Fatalf("Windows-side route not written: %v\n%s", err, text)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err == nil {
		t.Errorf("the native codex config was written by a Windows-only wire:\n%s", text)
	}
	for _, l := range lines {
		if strings.TrimSpace(l) != "" && !strings.HasPrefix(l, "codex-windows") {
			t.Errorf("a non-Windows result line: %q", l)
		}
	}
}

// TestWindowsHomeServiceRoutesThroughInitOptions: the seam detects homes with
// the doctor's detection, refuses off WSL, and applies with exactly the
// options the CLI flag sets (the tool selector + the home override), scoped
// to the Windows side.
func TestWindowsHomeServiceRoutesThroughInitOptions(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, h := range []string{a, b} {
		if err := os.MkdirAll(filepath.Join(h, ".cursor"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var got WireAIClientsOptions
	svc := &windowsHomeService{
		proxyPort: 8820,
		isWSL:     func() bool { return true },
		homes: func() []crossmount.HomeRoot {
			return []crossmount.HomeRoot{{Path: a, OS: crossmount.OSWindows}, {Path: b, OS: crossmount.OSWindows}}
		},
		owned: func(string) bool { return false },
		wire: func(o WireAIClientsOptions) ([]string, string, string, bool, error) {
			got = o
			return []string{"cursor-windows hook would register 3 hook(s)"}, "", "", false, nil
		},
	}
	wsl, targets, err := svc.Targets(context.Background())
	if err != nil || !wsl || len(targets) != 1 || targets[0].Target != "cursor-windows" || len(targets[0].Candidates) != 2 || targets[0].Resolved != "" {
		t.Fatalf("targets = %v %+v %v", wsl, targets, err)
	}
	lines, err := svc.Apply(context.Background(), dashapi.WindowsHomeApply{Target: "cursor-windows", Home: b, DryRun: true})
	if err != nil || len(lines) != 1 {
		t.Fatalf("apply = %v %v", lines, err)
	}
	if !got.OnlyCursor || got.WindowsCursorHome != b || !got.WindowsOnly || !got.DryRun || !got.SkipExtension || got.OnlyCodex || got.OnlyClaudeCode {
		t.Errorf("wire options = %+v", got)
	}
	if _, err := svc.Apply(context.Background(), dashapi.WindowsHomeApply{Target: "gemini-cli-windows", Home: b}); err == nil {
		t.Error("an unknown target must be refused")
	}
	svc.isWSL = func() bool { return false }
	if wsl, targets, _ := svc.Targets(context.Background()); wsl || len(targets) != 0 {
		t.Errorf("off WSL: wsl=%v targets=%v", wsl, targets)
	}
}
