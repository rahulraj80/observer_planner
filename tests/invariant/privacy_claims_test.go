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
// claims false; reintroducing one fails here. Since 2026-11-15 the
// content-free baseline usage telemetry is on by default, so "no usage
// statistics" and "does not collect usage data" are banned too: telemetry is
// described as four levels with baseline on (claims register C-060).
//
// Scope: files people read as product copy. Dated records (CHANGELOG
// entries, plans, handovers, audits, notes), tests, generated bundles and
// design mockups are out of scope. A narrowly scoped TRUE statement goes in
// privacyClaimAllow with its reason.
func TestNoAbsolutePrivacyClaims(t *testing.T) {
	root := repoRoot(t)
	claim := regexp.MustCompile(`(?i)\b(no|zero)[ -]telemetry\b|telemetry[ -]free|sends? no telemetry|no data leaves|nothing (you build here )?(ever )?leaves your (machine|laptop)|never leaves your (machine|laptop|computer)|100% local|zero outbound|no usage statistics|(does|do) not collect usage data|collects? no usage data|sends us nothing|nothing phones home|zero telemetry|nothing is sent anywhere|make no network calls - everything is local|no outbound channel|network calls by default \| none|anonymous (usage |baseline[- ])?telemetry|opt-in (usage |baseline )?telemetry|base usage opt-in|telemetry (is |stays )?(off|disabled) by default|no accounts?\. no cloud`)
	surfaces := []string{
		"README.md", "PRIVACY.md", "SECURITY.md", "docs", "npm", "pypi", "vscode",
		"web/src", "webcloud/src", "scripts/install.sh", "scripts/install.ps1",
		"cmd/observer/privacy.go", "website", "plugins", "browser-extension/manifest.json",
		"marketing", "installer", "notes/guide", "AGENTS.md", "CLAUDE.md",
		"website-redesign-color-options", "internal/config", "internal/telemetry",
		"cmd/observer/telemetry.go", "cmd/observer-cloud/killswitch.go",
		// Dated, but docs/README.md calls them the canonical message house to
		// reuse verbatim (2026-11-15 audit), so they are checked like live copy.
		"docs/product-story-review-2026-07-07.md",
		"docs/product-market-positioning-2026-07-07.md",
		"docs/product-feature-story-map-2026-07-07.md",
	}
	alwaysCheck := map[string]bool{
		"docs/product-story-review-2026-07-07.md":       true,
		"docs/product-market-positioning-2026-07-07.md": true,
		"docs/product-feature-story-map-2026-07-07.md":  true,
	}
	skipDir := regexp.MustCompile(`(^|/)(node_modules|dist|plans|handovers|audits|strategy|testdata|decisions|adr)(/|$)`)
	// Dated records: CHANGELOG history (and the website changelog/feed built
	// from it) and date-stamped docs (docs/*-YYYY-MM-DD.md) describe what was
	// true when written.
	skipFile := regexp.MustCompile(`(?i)(CHANGELOG\.md|changelog\.(md|html)|feed\.xml|search-index\.json|-20[0-9]{2}-[0-9]{2}-[0-9]{2}\.md|_test\.go|\.test\.tsx?|\.spec\.ts|\.map)$`)
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
			if skipFile.MatchString(rel) && !alwaysCheck[rel] {
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
	{"docs/website-design-guide.md", "Never claim", "the copy rule itself quotes the banned phrasings"},
	{"docs/website-design-guide.md", "Never write", "the copy rule itself quotes the banned phrasings"},
	{"docs/website-design-guide.md", `"telemetry-free"`, "the copy rule itself quotes the banned phrasings"},
	{"docs/website-design-guide.md", "collects no usage data.", "the copy rule itself quotes the banned phrasing (2026-11-15)"},
	{"docs/website/claims-register.md", "| vocabulary | Banned:", "upstream's claims register row C-007 quotes the banned phrasings to ban them"},
	{"docs/security.md", "install", "security ledger rows describing the installer, which contacts only the release host"},
	{"docs/observer-statusline.md", "telemetry vendor", "defines the statusline's own zero-network property"},
	{"docs/", "--no-telemetry", "names Claude Code's own --no-telemetry flag"},
	{"docs/vscode-extension-tracker.md", "", "dated tracker log of extension releases"},
	{"superbased-observer-vs-claude-code-usage-monitor", "", "quotes and assesses Claude Code Usage Monitor's own \"100% local\" claim"},
	{"superbased-observer-vs-tokentracker", "TokenTracker", "describes TokenTracker's own positioning"},
	{"docs/product-", "Telemetry wording superseded (15 Nov 2026)", "the banner quotes the retired phrasings so nobody reuses them"},
	{"", "work never leaves", "scoped to the user's work (prompts, code, files), which is true; usage telemetry is content-free"},
	{"", "history never leaves your machine", "scoped to the captured session history, which stays local"},
	{"cmd/observer/telemetry.go", "no telemetry endpoint configured", "an error message, not a claim"},
	{"docs/kiro-crew-adapter.md", "Crew ships anonymous telemetry", "describes Kiro Crew's own telemetry"},
	{"web/src/components/projectdetail/DeliveryTab.tsx", "Nothing is sent anywhere until you con", "scoped to the code-host connection"},
	{"web/src/lib/deliveryModel.ts", "remote-tracking refs", "scoped to local git refs"},
	{"marketing/video/storyboards/2026-07-11-explainer-local-by-default-v1.md", "(was \"No telemetry\" until 2026-11-15)", "records the retired chip text"},
	{"marketing/video/storyboards/2026-07-11-explainer-local-by-default-v1.md", "**CUT** — over-claim", "the forbidden-claims row quotes what was cut"},
	{"marketing/INFOGRAPHIC-STYLE.md", "history never leaves your machine", "example headline scoped to session history"},
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
