package vendorschema

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
)

// TestEmbeddedSchemasMatchPins keeps the embedded copies byte-identical to
// the pinned schemas the catalogue was generated from.
func TestEmbeddedSchemasMatchPins(t *testing.T) {
	for _, p := range agentconfig.SchemaPins() {
		raw, err := schemaFS.ReadFile("schemas/" + p.File)
		if err != nil {
			t.Fatalf("%s: not embedded (copy testdata/agentconfig/schemas/%s here): %v", p.Tool, p.File, err)
		}
		sum := sha256.Sum256(raw)
		if got := hex.EncodeToString(sum[:]); got != p.SHA256 {
			t.Errorf("%s: embedded %s sha256 %s != pinned %s (re-copy it from testdata/agentconfig/schemas)", p.Tool, p.File, got, p.SHA256)
		}
	}
}

func TestEverySchemaCompiles(t *testing.T) {
	for _, shape := range Shapes() {
		if _, err := schemaFor(shape); err != nil {
			t.Errorf("%s: %v", shape, err)
		}
	}
}

func TestCheck(t *testing.T) {
	cases := []struct {
		name        string
		shape       string
		tree        map[string]any
		wantIssue   bool
		wantUnknown bool
	}{
		{"claude ok", "claude-code/settings", map[string]any{"model": "opus"}, false, false},
		{"claude bad type", "claude-code/settings", map[string]any{"cleanupPeriodDays": "soon"}, true, false},
		{"codex unknown key", "codex/config", map[string]any{"zz_not_a_key": int64(1)}, true, true},
		{"codex ok", "codex/config", map[string]any{"model": "gpt-5"}, false, false},
		{"no schema", "aider/config", map[string]any{"anything": true}, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Check(c.shape, c.tree)
			if (len(got) > 0) != c.wantIssue {
				t.Fatalf("issues = %+v, want issue %v", got, c.wantIssue)
			}
			if c.wantUnknown && !got[0].Unknown {
				t.Fatalf("issue not classed unknown: %+v", got)
			}
		})
	}
}
