package invariant

import (
	"sort"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestHookWritersIntoCredentialFilesAreDeclared pins the D-268 rule: only a
// registry row carrying integration.HookSpec.HooksShareCredentialFile may
// read-modify-write a credential-bearing vendor file. Every span a hook (or
// statusline) writer owns is checked against the agentconfig catalogue: when
// the catalogue marks any setting in that file Secret, the row must carry
// the exception, so a new writer into such a file cannot ship without the
// credential-file rules. ZCode (repo grounding, docs/zcode-adapter.md) and
// Crush (the vendor schema's providers api_key, a catalogue row not marked
// Secret) are flagged on other grounding, so the converse is not asserted
// here; internal/hook pins the exact flagged set.
func TestHookWritersIntoCredentialFilesAreDeclared(t *testing.T) {
	secretShapes := map[string]bool{}
	for _, row := range agentconfig.Catalog() {
		if row.Secret {
			secretShapes[row.Tool+"\x00"+row.FileID] = true
		}
	}
	var missing []string
	seen := map[string]bool{}
	for _, s := range agentconfig.OwnedSpans() {
		if s.Owner != agentconfig.SpanOwnerHook && s.Owner != agentconfig.SpanOwnerStatusline {
			continue
		}
		if !secretShapes[s.Tool+"\x00"+s.Shape] {
			continue
		}
		seen[s.Tool] = true
		if !integration.HooksShareCredentialFile(s.Tool) {
			missing = append(missing, s.Tool+" ("+s.FileID+", "+s.Writer+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("hook writers rewrite a file the catalogue marks Secret without HooksShareCredentialFile: %v", missing)
	}
	if len(seen) == 0 {
		t.Fatal("no hook span landed in a Secret-bearing file: the cross-check is not looking at anything")
	}
	t.Logf("hook writers into Secret-bearing files: %v", keysOf(seen))
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
