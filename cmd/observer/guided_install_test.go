//go:build !sbo_omit_terminals

package main

import (
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/termsession"
)

// TestSetupWorkDirLadder pins the setup-PTY working-directory ladder: home,
// then the temp dir, then "" (inherit). A relative, blank or missing candidate
// is skipped, never used.
func TestSetupWorkDirLadder(t *testing.T) {
	t.Parallel()
	home, tmp := "/home/azureuser", "/tmp"
	if runtime.GOOS == "windows" { // an absolute path needs a volume there
		home, tmp = `C:\Users\azureuser`, `C:\Temp`
	}
	exists := func(dirs ...string) func(string) bool {
		return func(p string) bool {
			for _, d := range dirs {
				if p == d {
					return true
				}
			}
			return false
		}
	}
	cases := []struct {
		name      string
		home, tmp string
		isDir     func(string) bool
		want      string
	}{
		{"home wins", home, tmp, exists(home, tmp), home},
		{"missing home falls to tmp", home, tmp, exists(tmp), tmp},
		{"blank home falls to tmp", "  ", tmp, exists(tmp), tmp},
		{"relative home is never used", "azureuser", tmp, exists("azureuser", tmp), tmp},
		{"nothing usable inherits", home, tmp, exists(), ""},
		{"nil probe inherits", home, tmp, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := setupWorkDir(tc.home, tc.tmp, tc.isDir); got != tc.want {
				t.Errorf("setupWorkDir(%q, %q) = %q, want %q", tc.home, tc.tmp, got, tc.want)
			}
		})
	}
}

// specCapturingSpawner records every Spec the manager hands the spawner.
type specCapturingSpawner struct {
	mu    sync.Mutex
	specs []termsession.Spec
}

func (s *specCapturingSpawner) Spawn(spec termsession.Spec) (termsession.PTY, error) {
	s.mu.Lock()
	s.specs = append(s.specs, spec)
	s.mu.Unlock()
	return newFakePTY(), nil
}

// TestCreateSetupRunsInHomeNotDaemonCwd is the regression for the 2026-10-02
// goose guided-install failure on demo node 1: the daemon's cwd was "/", the
// setup PTY inherited it, and the vendor script's download into the current
// directory failed. Every setup command (install, uninstall, grants) must now
// start in the operator's home.
func TestCreateSetupRunsInHomeNotDaemonCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	sp := &specCapturingSpawner{}
	mgr := termsession.NewManager(termsession.Options{Spawner: sp})
	t.Cleanup(mgr.Shutdown)
	adapter := &launchManagerAdapter{mgr: mgr}

	for _, label := range []string{"install:goose", "uninstall:goose"} {
		if _, err := adapter.CreateSetup(dashapi.SetupSpec{
			Argv:  []string{"bash", "-lc", "true"},
			Label: label,
		}); err != nil {
			t.Fatalf("CreateSetup(%s): %v", label, err)
		}
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()
	if len(sp.specs) != 2 {
		t.Fatalf("spawned %d setup specs, want 2", len(sp.specs))
	}
	for _, spec := range sp.specs {
		if spec.Kind != termsession.SpecSetup {
			t.Errorf("%s: kind = %v, want SpecSetup", spec.SetupLabel, spec.Kind)
		}
		if spec.Dir != home {
			t.Errorf("%s: Dir = %q, want the operator home %q (never the daemon's inherited cwd)", spec.SetupLabel, spec.Dir, home)
		}
	}
}

// TestInstallPlanMissingPrereqs pins the prerequisite half of a guided install
// plan (kiro-cli on demo node 1, 2026-10-03: the vendor script died with
// "Missing required dependencies: unzip"): the chosen hint's grounded
// Requires are resolved through the SAME lookup argv[0] uses, a missing one is
// reported on the plan, and the preflight fold then withholds CanInstall and
// carries the remedy. Windows and a nil lookup never block.
func TestInstallPlanMissingPrereqs(t *testing.T) {
	t.Parallel()
	kiro, ok := integration.For("kiro-cli")
	if !ok || kiro.Binary == nil {
		t.Fatal("kiro-cli has no Binary row")
	}
	present := func(have ...string) installArgv0Lookup {
		return func(name string) (string, bool) {
			for _, h := range have {
				if h == name {
					return "/usr/bin/" + name, true
				}
			}
			return "", false
		}
	}

	plan, ok := dashboardInstallPlanFor(kiro.Binary.Installs, "linux", "/home/azureuser", present("bash", "curl", "sha256sum"))
	if !ok {
		t.Fatal("kiro-cli must have a Linux plan")
	}
	if !reflect.DeepEqual(plan.Missing, []string{"unzip"}) {
		t.Fatalf("Missing = %v, want [unzip]", plan.Missing)
	}

	var pf dashapi.ToolPreflight
	applyInstallPlan(&pf, plan, func() bool { return true })
	if pf.CanInstall {
		t.Error("a plan with a missing prerequisite must not be installable from the dashboard")
	}
	if pf.InstallCommand == "" {
		t.Error("the command stays on display even when blocked")
	}
	if !reflect.DeepEqual(pf.InstallMissingPrereqs, []string{"unzip"}) {
		t.Errorf("InstallMissingPrereqs = %v, want [unzip]", pf.InstallMissingPrereqs)
	}
	if !strings.Contains(pf.InstallPrereqRemedy, "unzip") || !strings.Contains(pf.InstallPrereqRemedy, "Re-check") {
		t.Errorf("remedy must name unzip and the next step: %q", pf.InstallPrereqRemedy)
	}

	full, _ := dashboardInstallPlanFor(kiro.Binary.Installs, "linux", "/home/azureuser", present("bash", "curl", "unzip", "sha256sum"))
	if len(full.Missing) != 0 {
		t.Errorf("all prerequisites present, Missing = %v", full.Missing)
	}
	var ok2 dashapi.ToolPreflight
	applyInstallPlan(&ok2, full, func() bool { return true })
	if !ok2.CanInstall || ok2.InstallPrereqRemedy != "" {
		t.Errorf("a ready host must be installable with no remedy: %+v", ok2)
	}

	if nilLook, _ := dashboardInstallPlanFor(kiro.Binary.Installs, "linux", "/home/azureuser", nil); len(nilLook.Missing) != 0 {
		t.Errorf("a nil lookup must never block, Missing = %v", nilLook.Missing)
	}

	goose, _ := integration.For("goose")
	gp, _ := dashboardInstallPlanFor(goose.Binary.Installs, "linux", "/home/azureuser", present("bash", "curl", "tar"))
	if !reflect.DeepEqual(gp.Missing, []string{"bzip2"}) {
		t.Errorf("goose without bzip2: Missing = %v, want [bzip2]", gp.Missing)
	}
}
