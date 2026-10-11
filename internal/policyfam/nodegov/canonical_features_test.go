package nodegov

import (
	"strings"
	"testing"
)

// TestCanonicalBodyKeepsTheFeatureBlock is the T-707 regression: the
// canonical body a publish signs (CompileBody's canonical bytes) must carry
// the feature.lock block, and the node's compile of those bytes must expand
// it again. Before the fix BodyFromPolicySpec dropped it, so a published
// feature lock never reached a node.
func TestCanonicalBodyKeepsTheFeatureBlock(t *testing.T) {
	_, canon, err := CompileBody([]byte(`{"schema":2,"features":{"codeintel":true,"guard":false}}`), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(canon), `"features":{"codeintel":true,"guard":false}`) {
		t.Fatalf("canonical body lost the feature block: %s", canon)
	}
	spec, again, err := CompileBody(canon, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(canon) {
		t.Fatalf("canonical form is not a fixed point:\n%s\n%s", canon, again)
	}
	if spec.Features["codeintel"] != true || spec.FeaturePinned["codeintel.enabled"] != true || spec.FeaturePinned["guard.enabled"] != false {
		t.Fatalf("node compile of the canonical body: features %v pins %v", spec.Features, spec.FeaturePinned)
	}
	if _, canon, err := CompileBody([]byte(`{"schema":2}`), 1<<20); err != nil || strings.Contains(string(canon), "features") {
		t.Fatalf("a body without features gained a block: %s (%v)", canon, err)
	}
}
