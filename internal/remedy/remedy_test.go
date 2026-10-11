package remedy

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryActionRendersForBothAudiences: every action in the closed set has
// a dashboard control, and a command unless it is one of the documented
// command-less fixes.
func TestEveryActionRendersForBothAudiences(t *testing.T) {
	commandless := map[Action]bool{CodexTrust: true, OrgClientEnable: true, RestartDaemon: true}
	for _, a := range Actions() {
		r := Of(a, "cursor")
		c, ok := ControlFor(r)
		if !ok || c.Label == "" || !strings.HasPrefix(c.Href, "/settings?") {
			t.Errorf("%s: control = %+v", a, c)
		}
		if got := Command(r); (got == "") != commandless[a] {
			t.Errorf("%s: command = %q", a, got)
		}
	}
	if Known("nope") || Command(Of("nope", "")) != "" {
		t.Error("an unknown action must render nothing")
	}
}

// TestCommandsMatchTheCLIVocabulary pins the spellings the findings embed.
func TestCommandsMatchTheCLIVocabulary(t *testing.T) {
	for _, tc := range []struct {
		r    Remediation
		want string
	}{
		{Of(Init, ""), "observer init"},
		{Of(InitTool, "codex"), "observer init --codex"},
		{Of(InitTool, "droid"), "observer init --all"},
		{Of(InitToolForce, "cursor"), "observer init --cursor --force"},
		{Of(InitToolRoute, "claude-code"), "observer init --claude-code --skip-hooks --skip-mcp"},
		{Of(InitMCP, ""), "observer init --skip-hooks --skip-proxy-route"},
		{Of(InitMCP, "codex"), "observer init --codex --skip-hooks --skip-proxy-route"},
		{Of(InitMCPForce, ""), "observer init --skip-hooks --skip-proxy-route --force"},
		{Of(UninstallTool, "claude-code"), "observer uninstall --claude-code"},
		{Of(InitWindowsHome, "cursor-windows"), "observer init --windows-cursor-home <path>"},
		{Of(InitWindowsRoute, "codex-windows"), "observer init"},
		{Of(OrgEnroll, ""), "observer enroll --link <url>"},
		{Of(OrgUnenroll, ""), "observer unenroll"},
		{Of(DoctorTool, ""), "observer doctor <tool>"},
		{Of(DoctorTool, "cursor"), "observer doctor cursor"},
		{Of(DoctorDBIntegrity, ""), "observer doctor db"},
	} {
		if got := Command(tc.r); got != tc.want {
			t.Errorf("Command(%+v) = %q, want %q", tc.r, got, tc.want)
		}
	}
	if q := Quote(Of(Init, "")); q != "`observer init`" {
		t.Errorf("Quote = %q", q)
	}
	if !Mentions("re-run `observer init` now", Of(Init, "")) || Mentions("nothing", Of(OrgUnenroll, "")) {
		t.Error("Mentions")
	}
}

// TestControlLinks pins the deep links the dashboard follows (the Connected
// tools wizard, its Remove panel, the Windows-home control, lane B's checks).
func TestControlLinks(t *testing.T) {
	for _, tc := range []struct {
		r    Remediation
		want string
	}{
		{Of(InitTool, "cursor"), "/settings?open=setup&section=tools&tool=cursor"},
		{Of(UninstallTool, "claude-code"), "/settings?open=remove&section=tools&tool=claude-code"},
		{Of(InitWindowsHome, "codex-windows"), "/settings?open=windows-home&section=tools&target=codex-windows"},
		{Of(DoctorTool, "cursor"), "/settings?open=doctor&section=tools&tool=cursor"},
		{Of(DoctorDBIntegrity, ""), "/settings?section=health#integrity"},
		{Of(RestartDaemon, ""), "/settings?section=health#daemon"},
	} {
		if c, _ := ControlFor(tc.r); c.Href != tc.want {
			t.Errorf("ControlFor(%+v).Href = %q, want %q", tc.r, c.Href, tc.want)
		}
	}
	if f, ok := WindowsHomeFlag("claude-code-windows"); !ok || f != "--windows-claude-home" {
		t.Error("WindowsHomeFlag")
	}
	if got := WindowsHomeTargets(); len(got) != 3 || got[0] != "claude-code-windows" {
		t.Errorf("WindowsHomeTargets = %v", got)
	}
}

// TestPackageIsPure keeps remedy free of I/O and of other observer packages.
func TestPackageIsPure(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		pf, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range pf.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == "database/sql" || p == "net/http" || p == "os" || p == "os/exec" || strings.Contains(p, "superbased-observer/internal/") || strings.Contains(p, "fsnotify") {
				t.Errorf("%s imports %s", f, p)
			}
		}
	}
}
