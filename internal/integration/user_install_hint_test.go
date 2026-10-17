package integration

import (
	"strings"
	"testing"
)

// userInstallHintJargon are words that mark an engineering record rather than
// user copy. A UserInstallHint is shown verbatim in the dashboard, so it must
// read like product copy: no provenance dates, no grounding vocabulary, no
// argv / config-key talk.
var userInstallHintJargon = []string{
	"grounded", "grounding", "argv", "2026-", "winget", "brew ", "InstallHint",
	"Routability", "WSLENV", "observer daemon", "—",
}

// TestUserInstallHintsArePlainCopy walks every BinaryResolveSpec the registry
// declares (terminal rows and GUI launch rows) and pins each non-empty
// UserInstallHint as one short, plain, finished sentence.
func TestUserInstallHintsArePlainCopy(t *testing.T) {
	type hinted struct{ owner, hint string }
	var hints []hinted
	for _, c := range Capabilities() {
		if c.Binary != nil && c.Binary.UserInstallHint != "" {
			hints = append(hints, hinted{c.Tool, c.Binary.UserInstallHint})
		}
	}
	for _, g := range GUILaunchables() {
		if g.Spec.Binary.UserInstallHint != "" {
			hints = append(hints, hinted{g.Spec.ID, g.Spec.Binary.UserInstallHint})
		}
	}
	if len(hints) == 0 {
		t.Fatal("no UserInstallHint is declared; the dashboard copy for the known platform-limited rows is missing")
	}
	for _, h := range hints {
		if len(h.hint) > 140 {
			t.Errorf("%s: UserInstallHint is %d chars, keep it under 140: %q", h.owner, len(h.hint), h.hint)
		}
		if !strings.HasSuffix(h.hint, ".") {
			t.Errorf("%s: UserInstallHint must be a finished sentence: %q", h.owner, h.hint)
		}
		for _, w := range userInstallHintJargon {
			if strings.Contains(h.hint, w) {
				t.Errorf("%s: UserInstallHint contains %q (engineering vocabulary): %q", h.owner, w, h.hint)
			}
		}
	}
}
