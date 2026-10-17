package nodegov

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestServedProjectionHashes (T-347): the set holds the hash an older node is
// served for the stored body (exactly the bytes ProjectServedBody signs), never
// the stored body's own hash, and is empty when no target strips anything.
func TestServedProjectionHashes(t *testing.T) {
	raw := []byte(`{"schema":1,"sections":{"hidden":["benchmarks","agent-config/organization"],"settings_hidden":["intel"]}}`)
	got := ServedProjectionHashes(raw, 1<<16)
	served, removed, err := ProjectServedBody(raw, 1<<16, TargetForAgentVersion("1.34.0-rc.11"))
	if err != nil || served == nil || len(removed) == 0 {
		t.Fatalf("an rc.11 node must be served a stripped body: %s %v %v", served, removed, err)
	}
	sum := sha256.Sum256(served)
	if !got[hex.EncodeToString(sum[:])] {
		t.Errorf("the rc.11 projection %s is missing from %v", served, got)
	}
	stored := sha256.Sum256(raw)
	if got[hex.EncodeToString(stored[:])] {
		t.Error("the stored body's own hash must not be in the projection set")
	}
	if h := ServedProjectionHashes([]byte(`{"schema":1}`), 1<<16); h != nil {
		t.Errorf("a body no target strips: %v, want nil", h)
	}
	if h := ServedProjectionHashes([]byte(`{`), 1<<16); h != nil {
		t.Errorf("an undecodable body: %v, want nil", h)
	}
}
