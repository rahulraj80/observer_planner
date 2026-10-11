package invariant

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestAgentConfigStateRowWireShape pins the org AI tool policy state row
// (adapter config P7) to its allow-list: closed codes, catalogue ids, value
// HASHES, a project identity hash, counts and timestamps. The ONE value-
// bearing field is ObservedValue, which ships only under shipsRawContent()
// and only scrubbed (T-428: a personal file's value ships too). A
// path, a project root, a free-text message or a new value field needs its
// own privacy review.
func TestAgentConfigStateRowWireShape(t *testing.T) {
	t.Parallel()
	allowed := map[string]bool{
		"Tool": true, "Setting": true, "FileID": true, "Scope": true, "ProjectHash": true,
		"Mode": true, "Layer": true, "State": true, "Reason": true,
		"ExpectedHash": true, "ObservedHash": true,
		"ObservedValue": true, // gated: full content only; secrets scrubbed (T-428)
		"DriftCount":    true, "LastDriftAt": true, "ObservedAt": true,
	}
	typ := reflect.TypeOf(orgcontract.AgentConfigStateRow{})
	if typ.NumField() != len(allowed) {
		t.Errorf("AgentConfigStateRow has %d fields, want exactly %d", typ.NumField(), len(allowed))
	}
	for i := 0; i < typ.NumField(); i++ {
		if name := typ.Field(i).Name; !allowed[name] {
			t.Errorf("AgentConfigStateRow gained field %q: the state wire carries codes, ids and hashes only", name)
		}
	}
}

// TestAgentConfigStateReportNeverShipsRootsOrRawSecrets builds a report from
// node-local rows carrying sentinels in every node-only field and proves what
// crosses: the local project root never; an observed value only with full
// content; under full content a personal (local-only) file's value too, and a
// secret-shaped value only scrubbed (D-112 "scrub instead of withhold",
// T-428). The secret row holds a RAW key on purpose (a writer that forgot to
// scrub): the report's own JSON-safe backstop must still keep it off the wire.
func TestAgentConfigStateReportNeverShipsRootsOrRawSecrets(t *testing.T) {
	t.Parallel()
	const root = "/home/dev/SENTINEL-ROOT/acme"
	const rawKey = "AKIAIOSFODNN7EXAMPLE"
	at := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	rows := []store.AgentConfigOrgStateRow{
		{
			SettingID: "codex/model", FileID: "codex/user-config", Tool: "codex", Scope: "user", Mode: "enforced",
			State: "drifted", ExpectedHash: "e1", ObservedHash: "o1", ObservedValue: `"SENTINEL-PLAIN"`, ObservedAt: at,
		},
		{
			SettingID: "codex/notify", FileID: "codex/user-config", Tool: "codex", Scope: "user", Mode: "enforced",
			State: "drifted", ExpectedHash: "e2", ObservedHash: "o2", ObservedValue: `["notify","--key","` + rawKey + `","SENTINEL-KEPT"]`, ObservedSecret: true, ObservedAt: at,
		},
		{
			SettingID: "claude-code/model", FileID: "claude-code/project-local-settings", Tool: "claude-code", Scope: "project_local",
			ProjectRoot: root, ProjectHash: "abc", Mode: "enforced", State: "drifted", ExpectedHash: "e3", ObservedHash: "o3",
			ObservedValue: `"SENTINEL-PERSONAL"`, Personal: true, ObservedAt: at,
		},
	}
	for _, full := range []bool{false, true} {
		rep := store.BuildAgentConfigStateReport(7, rows, full)
		b, err := json.Marshal(rep)
		if err != nil {
			t.Fatal(err)
		}
		wire := string(b)
		for _, never := range []string{"SENTINEL-ROOT", rawKey} {
			if strings.Contains(wire, never) {
				t.Errorf("full=%v: the report carries %s: %s", full, never, wire)
			}
		}
		for _, value := range []string{"SENTINEL-PLAIN", "SENTINEL-PERSONAL", "SENTINEL-KEPT"} {
			if got := strings.Contains(wire, value); got != full {
				t.Errorf("full=%v: observed value %s shipped=%v, want %v", full, value, got, full)
			}
		}
	}
}

// TestAgentConfigInventoryWireShape pins the managed node's AI tool
// configuration inventory (D-112, T-427): the report carries a digest, a
// truncation flag and rows of the SAME allow-listed state row type (so the
// row allow-list above stays the one privacy review), and the envelope key is
// omitempty, so an individual node's (and an unchanged inventory's) envelope
// stays byte-identical. The composer's gates (managed enrolment, raw content
// only, changed digest only) are pinned by internal/store's
// TestComposeAgentConfigInventory; the node builder's scrubbing and its
// no-local-path rule by internal/agentconfigsvc's TestInventory.
func TestAgentConfigInventoryWireShape(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(orgcontract.AgentConfigInventoryReport{})
	allowed := map[string]bool{"Digest": true, "Rows": true, "Truncated": true}
	if typ.NumField() != len(allowed) {
		t.Errorf("AgentConfigInventoryReport has %d fields, want exactly %d", typ.NumField(), len(allowed))
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !allowed[f.Name] {
			t.Errorf("AgentConfigInventoryReport gained field %q: it needs its own privacy review", f.Name)
		}
		if f.Name == "Rows" && f.Type != reflect.TypeOf([]orgcontract.AgentConfigStateRow{}) {
			t.Errorf("inventory rows must be the allow-listed state row type, got %v", f.Type)
		}
	}
	raw, _ := json.Marshal(orgcontract.PushEnvelope{})
	if strings.Contains(string(raw), "agent_config_inventory") {
		t.Fatalf("an empty envelope carries the inventory key: %s", raw)
	}
}
