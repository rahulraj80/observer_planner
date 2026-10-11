package nodegov

import (
	"fmt"
	"strings"
	"testing"
)

// TestSchemaHintDecodes is the drift guard for SchemaHint — see the egress
// package's equivalent for the rationale. The body uses EVERY hinted key at
// MaxSchema, with an expose list populated so the newest schema is actually
// required (Sol F6: the hint used to say "currently 2" and omit the schema-3
// expose lists, so a reviewer following it would drop them and re-hide pages).
func TestSchemaHintDecodes(t *testing.T) {
	hint := SchemaHint()
	if hint == "" {
		t.Fatal("nodegov.SchemaHint() is empty")
	}
	if !strings.Contains(hint, fmt.Sprintf("currently %d", MaxSchema)) {
		t.Errorf("hint does not name MaxSchema %d: %s", MaxSchema, hint)
	}
	for _, key := range []string{"hidden", "read_only", "settings_hidden", "settings_read_only", "visible", "settings_visible"} {
		if !strings.Contains(hint, `"`+key+`"`) {
			t.Errorf("hint does not name sections key %q", key)
		}
	}
	body := fmt.Sprintf(`{
		"schema": %d,
		"sections": {"hidden": [], "read_only": [], "settings_hidden": [], "settings_read_only": [], "visible": ["security"], "settings_visible": ["guard"]},
		"pinned": {},
		"share": {},
		"features": {},
		"notice": {"org_display_name": "", "contact": "", "policy_url": ""}
	}`, MaxSchema)
	b, err := DecodeBody([]byte(body), 1<<20)
	if err != nil {
		t.Fatalf("hinted-keys body failed to decode (schema drifted from SchemaHint?): %v", err)
	}
	if SchemaFor(b) != MaxSchema {
		t.Fatalf("the drift body needs schema %d, MaxSchema is %d: extend it with the newest list", SchemaFor(b), MaxSchema)
	}
}
