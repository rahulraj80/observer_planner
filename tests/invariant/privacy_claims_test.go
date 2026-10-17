package invariant

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoAbsolutePrivacyClaims is the drift gate for the 2026-10-17 copy
// change: user-facing surfaces state what actually leaves the machine
// (PRIVACY.md, `observer privacy`) instead of absolute claims such as "no
// telemetry" or "nothing leaves your machine". A default-on network call
// (the model price table refresh, the proxy TLS warm-up) already made those
// claims false; reintroducing one fails here.
//
// Scope: files people read as product copy. Dated records (CHANGELOG
// entries, plans, handovers, audits, notes), tests, generated bundles and
// design mockups are out of scope. A narrowly scoped TRUE statement goes in
// privacyClaimAllow with its reason.
func TestNoAbsolutePrivacyClaims(t *testing.T) {
	root := repoRoot(t)
	claim := regexp.MustCompile(`(?i)\b(no|zero)[ -]telemetry\b|telemetry[ -]free|sends? no telemetry|no data leaves|nothing (you build here )?(ever )?leaves your (machine|laptop)|never leaves your (machine|laptop|computer)|100% local|zero outbound`)
	surfaces := []string{
		"README.md", "PRIVACY.md", "SECURITY.md", "docs", "npm", "pypi", "vscode",
		"web/src", "webcloud/src", "scripts/install.sh", "scripts/install.ps1",
		"cmd/observer/privacy.go", "website", "plugins", "browser-extension/manifest.json",
	}
	skipDir := regexp.MustCompile(`(^|/)(node_modules|dist|plans|handovers|audits|strategy|testdata)(/|$)`)
	skipFile := regexp.MustCompile(`(?i)(CHANGELOG\.md|_test\.go|\.test\.tsx?|\.spec\.ts|\.map)$`)
	var bad []string
	for _, s := range surfaces {
		base := filepath.Join(root, s)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		_ = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if skipDir.MatchString(rel) {
					return filepath.SkipDir
				}
				return nil
			}
			if skipFile.MatchString(rel) {
				return nil
			}
			switch strings.ToLower(filepath.Ext(rel)) {
			case ".md", ".html", ".tsx", ".ts", ".json", ".toml", ".txt", ".go", ".sh", ".ps1", ".xml", ".mjs":
			default:
				return nil
			}
			body, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			for i, line := range strings.Split(string(body), "\n") {
				if m := claim.FindString(line); m != "" && !privacyClaimAllowed(rel, line) {
					bad = append(bad, rel+":"+itoa(int64(i+1))+": "+strings.TrimSpace(trunc(line, 140)))
				}
			}
			return nil
		})
	}
	if len(bad) > 0 {
		t.Errorf("%d absolute privacy claim(s) on user-facing surfaces; state what leaves the machine instead (PRIVACY.md lists every call):\n  %s",
			len(bad), strings.Join(bad, "\n  "))
	}
}

// privacyClaimAllow: (path substring, line substring) pairs for scoped
// statements that are TRUE as written.
var privacyClaimAllow = []struct{ path, line, why string }{
	{"vscode/", "telemetry.telemetryLevel", "describes the extension honouring VS Code's own telemetry setting"},
	{"webcloud/src/", "until you preview it and approve it", "cloud portal: nothing is uploaded before the developer approves the preview"},
	{"webcloud/src/components/AuthShell.tsx", "TRUST_ROW", "the code comment describing that same scoped portal promise"},
	{"browser-extension/manifest.json", "", "the browser extension itself sends nothing; it only talks to the local observer"},
}

func privacyClaimAllowed(rel, line string) bool {
	for _, a := range privacyClaimAllow {
		if strings.Contains(rel, a.path) && strings.Contains(line, a.line) {
			return true
		}
	}
	return false
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
