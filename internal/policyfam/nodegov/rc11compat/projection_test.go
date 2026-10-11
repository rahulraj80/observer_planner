package rc11compat

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	head "github.com/marmutapp/superbased-observer/internal/policyfam/nodegov"
)

// maxBody is the wire cap both sides use.
const maxBody = 256 << 10

// corpusBody is one node.governance body the org server can serve: it passes
// TODAY's strict compiler (the publish lint, and the compile every composed
// override body goes through), so it is exactly the set of bodies a released
// node can be handed.
type corpusBody struct {
	name string
	body head.Body
}

// pinValue picks a value the current compiler accepts for a pinnable key.
func pinValue(k head.PinnableKey) any {
	switch k.Kind {
	case "bool":
		if k.Direction == head.DirRestrictiveOnly {
			return k.Safe
		}
		return true
	case "string":
		if len(k.Enum) > 0 {
			return k.Enum[len(k.Enum)-1]
		}
		return "x"
	case "int":
		if k.Min != 0 {
			return float64(k.Min)
		}
		return float64(1)
	default:
		return []any{}
	}
}

func shareValue(k head.ShareKey) any {
	if k.Kind == "bool" {
		return true
	}
	return []any{}
}

func compilesToday(b head.Body) bool {
	raw, err := json.Marshal(b)
	if err != nil {
		return false
	}
	_, _, err = head.CompileBody(raw, maxBody)
	return err == nil
}

// buildCorpus returns one body per directive the current vocabulary has (every
// section and tab id in every list it may appear in, every pinned key, share
// key and feature), plus maximal combinations. The schema is what a publisher
// stamps (SchemaFor), and a second copy of each combined body declares
// schema 3 explicitly, which the publish lint also accepts.
func buildCorpus(t *testing.T) []corpusBody {
	t.Helper()
	var out []corpusBody
	add := func(name string, b head.Body) {
		b.Schema = head.SchemaFor(b)
		if compilesToday(b) {
			out = append(out, corpusBody{name, b})
		}
	}
	for _, id := range head.NavSectionIDs {
		if head.IsUnhideableNavSection(id) {
			continue
		}
		add("hidden "+id, head.Body{Sections: head.SectionsV1{Hidden: []string{id}}})
		add("read_only "+id, head.Body{Sections: head.SectionsV1{ReadOnly: []string{id}}})
		add("visible "+id, head.Body{Sections: head.SectionsV1{Visible: []string{id}}})
	}
	for _, id := range head.NavTabIDs() {
		add("hidden tab "+id, head.Body{Sections: head.SectionsV1{Hidden: []string{id}}})
	}
	for _, id := range head.SettingsSectionIDs {
		if head.IsUnhideableSettingsSection(id) {
			continue
		}
		add("settings_hidden "+id, head.Body{Sections: head.SectionsV1{SettingsHidden: []string{id}}})
		add("settings_read_only "+id, head.Body{Sections: head.SectionsV1{SettingsReadOnly: []string{id}}})
		add("settings_visible "+id, head.Body{Sections: head.SectionsV1{SettingsVisible: []string{id}}})
	}
	pins := map[string]any{}
	for _, k := range head.PinnableKeys {
		b := head.Body{Pinned: map[string]any{k.Key: pinValue(k)}}
		if compilesToday(head.Body{Schema: 2, Pinned: b.Pinned}) {
			pins[k.Key] = pinValue(k)
		}
		add("pinned "+k.Key, b)
	}
	shares := map[string]any{}
	for _, k := range head.ShareKeys {
		b := head.Body{Share: map[string]any{k.Key: shareValue(k)}}
		if compilesToday(head.Body{Schema: 2, Share: b.Share}) {
			shares[k.Key] = shareValue(k)
		}
		add("share "+k.Key, b)
	}
	features := map[string]bool{}
	for _, f := range head.Features {
		v, _ := pins[f.Key].(bool)
		add("feature "+f.ID, head.Body{Features: map[string]bool{f.ID: v}})
		features[f.ID] = v
	}
	if len(out) < 150 {
		t.Fatalf("corpus has %d single-directive bodies: the generator lost the vocabulary", len(out))
	}

	var hideableNav, hideableSettings []string
	for _, id := range head.NavSectionIDs {
		if !head.IsUnhideableNavSection(id) && id != "security" {
			hideableNav = append(hideableNav, id)
		}
	}
	for _, id := range head.SettingsSectionIDs {
		if !head.IsUnhideableSettingsSection(id) {
			hideableSettings = append(hideableSettings, id)
		}
	}
	combos := []corpusBody{
		{"everything hidden and pinned", head.Body{
			Sections: head.SectionsV1{Hidden: hideableNav, SettingsHidden: hideableSettings},
			Pinned:   pins, Share: shares, Features: features,
			Notice: &head.NoticeV1{OrgDisplayName: "Acme", Contact: "it@acme.example"},
		}},
		{"everything locked", head.Body{
			Sections: head.SectionsV1{ReadOnly: hideableNav, SettingsReadOnly: hideableSettings},
			Pinned:   pins,
		}},
		{"everything exposed, every tab hidden", head.Body{
			Sections: head.SectionsV1{Visible: append([]string{"security"}, hideableNav...), SettingsVisible: hideableSettings, Hidden: head.NavTabIDs()},
			Share:    shares,
		}},
		{"the T-132 example: show Security, hide its Prompt guard tab", head.Body{
			Sections: head.SectionsV1{Hidden: []string{"benchmarks", "security/prompt-guard"}, Visible: []string{"security"}},
		}},
	}
	for _, c := range combos {
		c.body.Schema = head.SchemaFor(c.body)
		if !compilesToday(c.body) {
			raw, _ := json.Marshal(c.body)
			_, _, err := head.CompileBody(raw, maxBody)
			t.Fatalf("combined corpus body %q does not compile today: %v", c.name, err)
		}
		out = append(out, c)
		declared3 := c.body
		declared3.Schema = 3
		if compilesToday(declared3) {
			out = append(out, corpusBody{c.name + " (declared schema 3)", declared3})
		}
	}
	return out
}

// served is what the org server hands a node at target t: the projection, or
// the stored body untouched when nothing needs removing.
func served(t *testing.T, raw []byte, target head.Target) []byte {
	t.Helper()
	projected, _, err := head.ProjectServedBody(raw, maxBody, target)
	if err != nil {
		t.Fatalf("ProjectServedBody: %v", err)
	}
	if projected == nil {
		return raw
	}
	return projected
}

// TestReleasedRC11RefusesBodiesThatNeedProjection is the defect, reproduced
// against the released compiler: a body that exposes a page (schema 3), hides
// a tab, names the agent-config page or pins the push interval is refused
// WHOLE by an rc.11 node, so serving it unprojected would cut that node off
// from every governance change.
func TestReleasedRC11RefusesBodiesThatNeedProjection(t *testing.T) {
	cases := []struct{ name, body, want string }{
		// rc.11's wire struct has no expose lists, so DisallowUnknownFields
		// refuses the field itself, before the schema gate.
		{"an expose list", `{"schema":3,"sections":{"hidden":["benchmarks"],"visible":["security"]}}`, `unknown field "visible"`},
		{"a declared schema 3", `{"schema":3,"sections":{"hidden":["benchmarks"]}}`, "schema 3"},
		{"a page tab", `{"schema":1,"sections":{"hidden":["security/prompt-guard"]}}`, "security/prompt-guard"},
		{"the agent-config page", `{"schema":1,"sections":{"hidden":["agent-config"]}}`, "agent-config"},
		{"the push-interval pin", `{"schema":2,"pinned":{"org_client.push_interval_seconds":300}}`, "org_client.push_interval_seconds"},
		// Level 6 (Integration 6): the org Cloud Intelligence settings
		// section and the AI tool settings Organization tab. ("requests",
		// the other level-6 id, is on the T8 floor, so no body names it.)
		{"the intel settings section", `{"schema":1,"sections":{"settings_hidden":["intel"]}}`, "intel"},
		{"the AI tool settings Organization tab", `{"schema":1,"sections":{"hidden":["agent-config/organization"]}}`, "agent-config/organization"},
	}
	for _, tc := range cases {
		if _, _, err := head.CompileBody([]byte(tc.body), maxBody); err != nil {
			t.Fatalf("%s: today's compiler must accept %s: %v", tc.name, tc.body, err)
		}
		_, _, err := CompileBody([]byte(tc.body), maxBody)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: rc.11 CompileBody err = %v, want a refusal naming %q", tc.name, err, tc.want)
		}
	}
}

// TestLevelSixIDsAreProjectedAwayBelowSix pins the Integration 6 vocabulary
// level: the ids that joined at level 6 (nav "requests", Settings "intel",
// the "agent-config/organization" tab) are removed from a body served to a
// level-5 node (a tabs-lane build) and to the released v1.34 line, and kept
// for a level-6 node. Every other id in the body survives.
func TestLevelSixIDsAreProjectedAwayBelowSix(t *testing.T) {
	if head.CurrentVocabLevel != 6 {
		t.Fatalf("CurrentVocabLevel = %d; this test pins level 6", head.CurrentVocabLevel)
	}
	if got := head.NavSectionSince("requests"); got != 6 {
		t.Errorf("NavSectionSince(requests) = %d, want 6", got)
	}
	if got := head.SettingsSectionSince("intel"); got != 6 {
		t.Errorf("SettingsSectionSince(intel) = %d, want 6", got)
	}
	if got := head.NavSectionSince("agent-config/organization"); got != 6 {
		t.Errorf("NavSectionSince(agent-config/organization) = %d, want 6", got)
	}
	raw := []byte(`{"schema":1,"sections":{"hidden":["benchmarks","agent-config/organization"],"settings_hidden":["intel","cloud"]}}`)
	if _, _, err := head.CompileBody(raw, maxBody); err != nil {
		t.Fatalf("today's compiler must accept the body: %v", err)
	}
	for _, tc := range []struct {
		name   string
		target head.Target
		kept   bool
	}{
		{"level 5 (tabs lane)", head.Target{Level: 5, MaxSchema: 3, KeyGen: 4}, false},
		{"released v1.34", head.TargetForAgentVersion("1.34.0"), false},
		{"level 6", head.Target{Level: 6, MaxSchema: 3, KeyGen: 4}, true},
	} {
		body := string(served(t, raw, tc.target))
		for _, id := range []string{"agent-config/organization", `"intel"`} {
			if strings.Contains(body, id) != tc.kept {
				t.Errorf("%s: served %s, want %s kept=%v", tc.name, body, id, tc.kept)
			}
		}
		for _, id := range []string{"benchmarks", `"cloud"`} {
			if !strings.Contains(body, id) {
				t.Errorf("%s: served %s dropped %s, which every level compiles", tc.name, body, id)
			}
		}
	}
}

// TestTargetsForTheReleasedV134Line pins what the server resolves for a node
// that sends no capability token and reports a v1.34.0 version: the rc.11
// fixture's own vocabulary level, schema and key generation, whatever the
// pre-release or build suffix.
func TestTargetsForTheReleasedV134Line(t *testing.T) {
	want := head.Target{Level: 3, MaxSchema: MaxSchema, KeyGen: 3}
	if MaxSchema != 2 {
		t.Fatalf("fixture MaxSchema = %d, want the released 2", MaxSchema)
	}
	for _, v := range []string{"1.34.0-rc.1", "v1.34.0-rc.11", "1.34.0", "1.34.0-rc.12+node1.017ca5210"} {
		if got := head.TargetForAgentVersion(v); got != want {
			t.Errorf("TargetForAgentVersion(%q) = %+v, want %+v", v, got, want)
		}
	}
	if got := head.LastUncappedTarget(); got != want {
		t.Errorf("LastUncappedTarget = %+v, want %+v (a member that never pushed)", got, want)
	}
}

// TestReleasedRC11AcceptsEveryProjectedBody runs every body in the corpus
// through the serve-time projection for each version the v1.34 line reports,
// and then through the RELEASED rc.11 compiler. Every one must compile, and
// every directive rc.11 knows must survive with its value: the projection
// removes only what rc.11 cannot compile (plus the expose lists, which rc.11
// has no managed defaults for).
func TestReleasedRC11AcceptsEveryProjectedBody(t *testing.T) {
	corpus := buildCorpus(t)
	targets := map[string]head.Target{
		"rc.11 by version": head.TargetForAgentVersion("1.34.0-rc.11"),
		"rc.1 by version":  head.TargetForAgentVersion("1.34.0-rc.1"),
		"never pushed":     head.LastUncappedTarget(),
	}
	projectedCount := 0
	for tname, target := range targets {
		for _, c := range corpus {
			raw, err := json.Marshal(c.body)
			if err != nil {
				t.Fatal(err)
			}
			body := served(t, raw, target)
			if !reflect.DeepEqual(body, raw) {
				projectedCount++
			}
			spec, _, err := CompileBody(body, maxBody)
			if err != nil {
				t.Errorf("%s / %s: rc.11 refuses the served body %s: %v", tname, c.name, body, err)
				continue
			}
			for _, miss := range lostDirectives(c.body, spec) {
				t.Errorf("%s / %s: %s was dropped, but rc.11 compiles it", tname, c.name, miss)
			}
		}
	}
	if projectedCount == 0 {
		t.Fatal("no body needed projecting: the corpus no longer exercises the projection")
	}
	t.Logf("%d corpus bodies x %d targets, %d served projected", len(corpus), len(targets), projectedCount)
}

// lostDirectives lists every directive of full that rc.11 compiles (its own
// vocabularies decide) but that the compiled served spec does not carry.
func lostDirectives(full head.Body, got PolicySpec) []string {
	var lost []string
	check := func(list string, ids []string, known func(string) bool, have []string) {
		set := map[string]bool{}
		for _, id := range have {
			set[id] = true
		}
		for _, id := range ids {
			if known(id) && !set[id] {
				lost = append(lost, list+":"+id)
			}
		}
	}
	check("sections.hidden", full.Sections.Hidden, IsNavSection, got.HiddenSections)
	check("sections.read_only", full.Sections.ReadOnly, IsNavSection, got.ReadOnlySections)
	check("sections.settings_hidden", full.Sections.SettingsHidden, IsSettingsSection, got.HiddenSettings)
	check("sections.settings_read_only", full.Sections.SettingsReadOnly, IsSettingsSection, got.ReadOnlySettings)
	for _, k := range sortedKeys(full.Pinned) {
		if _, ok := LookupPinnableKey(k); ok && fmt.Sprint(got.Pinned[k]) != fmt.Sprint(normalizeForCompare(full.Pinned[k])) {
			lost = append(lost, "pinned:"+k)
		}
	}
	for _, k := range sortedKeys(full.Share) {
		if _, ok := LookupShareKey(k); ok && fmt.Sprint(got.Share[k]) != fmt.Sprint(normalizeForCompare(full.Share[k])) {
			lost = append(lost, "share:"+k)
		}
	}
	// A feature is a compile-time alias of its pin, and the canonical body
	// carries it AS that pin (BodyFromPolicySpec), so what must survive is the
	// pinned value it expands to.
	for _, id := range sortedKeys(full.Features) {
		if f, ok := LookupFeature(id); ok {
			v, has := got.Pinned[f.Key]
			if !has {
				v, has = got.FeaturePinned[f.Key]
			}
			if !has || v != full.Features[id] {
				lost = append(lost, "features:"+id)
			}
		}
	}
	if full.Notice != nil && (got.Notice.OrgDisplayName != full.Notice.OrgDisplayName || got.Notice.Contact != full.Notice.Contact) {
		lost = append(lost, "notice")
	}
	sort.Strings(lost)
	return lost
}

// normalizeForCompare maps a JSON number onto the int64 the compiler stores.
func normalizeForCompare(v any) any {
	if f, ok := v.(float64); ok {
		return int64(f)
	}
	return v
}
