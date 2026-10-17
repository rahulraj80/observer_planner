package nodegov

import (
	"encoding/json"
	"strings"
	"testing"
)

// firstLevelNav / firstLevelSettings are the vocabularies v1.31.0 shipped
// (git show v1.31.0:internal/policyfam/nodegov/vocab.go). They are literal on
// purpose: the level table is a claim about what RELEASED builds compile, so
// it is checked against what they actually compiled, not against today's
// lists.
var (
	firstLevelNav = []string{
		"overview", "live", "sessions", "actions", "security", "egress", "search",
		"cost", "analysis", "tools",
		"compression", "cache", "suggestions", "routing", "benchmarks", "discovery", "patterns",
		"policies", "privacy", "terminals", "remote", "settings",
	}
	firstLevelSettings = []string{
		"pricing", "backfill", "tools", "health", "storage", "enrolment", "intelligence",
		"observer", "watcher", "freshness", "retention", "hooks", "proxy", "dashboard",
		"compression", "profiles", "org", "guard", "routing", "otel", "mcp", "advisor",
		"cachetrack", "observability", "secrets", "antigravity", "process", "browser",
		"terminal",
	}
	// firstLevelTabs is the tab vocabulary FirstTabVocabLevel shipped with
	// (navtabs_gen.go at T-132). Literal for the same reason as the two lists
	// above: it is a claim about what level-5 builds compile.
	firstLevelTabs = []string{
		"security/overview", "security/verdicts", "security/rules", "security/budgets",
		"security/prompt-guard", "security/mcp", "security/coverage",
		"cost/overview", "cost/by-model", "cost/budgets", "cost/statement", "cost/reconciliation",
		"compression/savings", "compression/events", "compression/experiments", "compression/advanced",
		"cache/overview", "cache/events",
		"routing/overview", "routing/decisions", "routing/policy", "routing/shadow",
		"policies/overview", "policies/templates", "policies/guardrails", "policies/routing",
		"policies/test", "policies/activity",
		"agent-config/tools", "agent-config/projects", "agent-config/common",
		"agent-config/instructions", "agent-config/coverage", "agent-config/history",
		"terminals/workspace", "terminals/settings", "terminals/arena",
		"remote/pairing", "remote/terminal-control", "remote/tailscale", "remote/audit",
	}
)

// TestVocabLevelsCoverEveryID fails when a section id joins a vocabulary
// without a since-level: the server would then serve it to builds that fail
// the whole body on it.
func TestVocabLevelsCoverEveryID(t *testing.T) {
	check := func(name string, current, first []string, since map[string]int) {
		base := setOf(first)
		cur := setOf(current)
		for _, id := range current {
			lvl, has := since[id]
			switch {
			case base[id] && has:
				t.Errorf("%s %q is in the first-level vocabulary but also carries since=%d", name, id, lvl)
			case !base[id] && !has:
				t.Errorf("%s %q joined after v1.31.0 but has no since-level: add it to the since table and bump CurrentVocabLevel", name, id)
			case has && (lvl <= FirstVocabLevel || lvl > CurrentVocabLevel):
				t.Errorf("%s %q since=%d is outside (%d, %d]", name, id, lvl, FirstVocabLevel, CurrentVocabLevel)
			}
		}
		for id := range since {
			if !cur[id] {
				t.Errorf("%s since-table names %q, which is not in the vocabulary", name, id)
			}
		}
	}
	check("nav", NavSectionIDs, firstLevelNav, navSince)
	check("settings", SettingsSectionIDs, firstLevelSettings, settingsSince)

	// Tabs: every tab this build knows is at FirstTabVocabLevel unless
	// tabSince names a later level, and tabSince names only real tabs.
	for id, lvl := range tabSince {
		if !IsNavTab(id) {
			t.Errorf("tabSince names %q, which is not a known tab", id)
		}
		if lvl <= FirstTabVocabLevel || lvl > CurrentVocabLevel {
			t.Errorf("tab %q since=%d is outside (%d, %d]", id, lvl, FirstTabVocabLevel, CurrentVocabLevel)
		}
	}
	for _, id := range firstLevelTabs {
		if !IsNavTab(id) {
			t.Errorf("firstLevelTabs names %q, which this build no longer knows: a tab was renamed or removed, and nodes at level %d still hide it by that id", id, FirstTabVocabLevel)
		}
	}
	firstTabs := setOf(firstLevelTabs)
	for _, id := range NavTabIDs() {
		if _, has := tabSince[id]; !firstTabs[id] && !has {
			t.Errorf("tab %q joined after level %d but has no since-level: add it to tabSince at a new level and bump CurrentVocabLevel", id, FirstTabVocabLevel)
		}
	}

	maxSince := FirstTabVocabLevel
	for _, table := range []map[string]int{navSince, settingsSince, tabSince} {
		for _, lvl := range table {
			if lvl > maxSince {
				maxSince = lvl
			}
		}
	}
	if maxSince != CurrentVocabLevel {
		t.Errorf("highest since-level is %d but CurrentVocabLevel is %d", maxSince, CurrentVocabLevel)
	}
	if LastUncappedVocabLevel >= CurrentVocabLevel {
		t.Errorf("LastUncappedVocabLevel %d must be below CurrentVocabLevel %d: this build advertises its level", LastUncappedVocabLevel, CurrentVocabLevel)
	}
	if got := LastUncappedTarget().Level; got != LastUncappedVocabLevel {
		t.Errorf("newest version row maps to level %d, want LastUncappedVocabLevel %d", got, LastUncappedVocabLevel)
	}
}

func TestParseVocabCapability(t *testing.T) {
	cases := []struct {
		name   string
		tokens []string
		want   int
		ok     bool
	}{
		{"absent (an older build)", []string{"selectors"}, 0, false},
		{"none at all", nil, 0, false},
		{"this build", []string{"selectors", VocabCapabilityToken(CurrentVocabLevel)}, CurrentVocabLevel, true},
		{"an older level", []string{"nodegov-vocab-2"}, 2, true},
		{"highest of several", []string{"nodegov-vocab-2", " nodegov-vocab-3 "}, 3, true},
		{"newer than this server is clamped", []string{"nodegov-vocab-99"}, CurrentVocabLevel, true},
		{"garbage level ignored", []string{"nodegov-vocab-x", "nodegov-vocab-0"}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseVocabCapability(tc.tokens)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("ParseVocabCapability(%v) = %d,%v want %d,%v", tc.tokens, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestVocabLevelForAgentVersion pins the section-vocabulary axis of the
// version fallback (the other two axes: target_test.go).
func TestVocabLevelForAgentVersion(t *testing.T) {
	cases := []struct {
		version string
		want    int
	}{
		{"1.31.0", 1},
		{"1.32.4", 1},
		{"1.33.0", 2},
		{"1.33.9", 2},
		{"1.34.0-rc.1", 3}, // the first build with "projects": every v1.34.0 rc had it
		{"1.34.0", 3},
		{"1.35.2", 3},
		{"", FirstVocabLevel},
		{"dev", FirstVocabLevel},
	}
	for _, tc := range cases {
		if got := TargetForAgentVersion(tc.version).Level; got != tc.want {
			t.Errorf("TargetForAgentVersion(%q).Level = %d, want %d", tc.version, got, tc.want)
		}
	}
}

// TestProjectToVocab pins what each level receives, and that the projected
// body only ever names ids that level compiles.
func TestProjectToVocab(t *testing.T) {
	full := Body{
		Schema: 3,
		Sections: SectionsV1{
			Hidden:         []string{"cost", "projects"},
			ReadOnly:       []string{"policies"},
			SettingsHidden: []string{"tasks", "process"},
			Visible:        []string{"agent-config"},
		},
	}
	cases := []struct {
		level          int
		hidden         []string
		settingsHidden []string
		visible        []string
		schema         int
		removed        int
	}{
		// removed counts the ids plus the "schema:3->1" lowering entry.
		{CurrentVocabLevel, []string{"cost", "projects"}, []string{"tasks", "process"}, []string{"agent-config"}, 3, 0},
		{3, []string{"cost", "projects"}, []string{"tasks", "process"}, nil, 1, 2},
		{2, []string{"cost"}, []string{"tasks", "process"}, nil, 1, 3},
		{1, []string{"cost"}, []string{"process"}, nil, 1, 4},
	}
	for _, tc := range cases {
		got, removed := ProjectToVocab(full, tc.level)
		if strings.Join(got.Sections.Hidden, ",") != strings.Join(tc.hidden, ",") ||
			strings.Join(got.Sections.SettingsHidden, ",") != strings.Join(tc.settingsHidden, ",") ||
			strings.Join(got.Sections.Visible, ",") != strings.Join(tc.visible, ",") ||
			got.Schema != tc.schema || len(removed) != tc.removed {
			t.Errorf("level %d: got %+v schema %d removed %v", tc.level, got.Sections, got.Schema, removed)
		}
		for _, ref := range sectionListRefs(&got.Sections) {
			for _, id := range *ref.ids {
				if ref.since(id) > tc.level {
					t.Errorf("level %d: projected body still names %s %q (since %d)", tc.level, ref.name, id, ref.since(id))
				}
			}
		}
	}
	if len(full.Sections.Hidden) != 2 || len(full.Sections.Visible) != 1 || full.Schema != 3 {
		t.Fatalf("ProjectToVocab modified its input: %+v", full)
	}
}

// TestCompileBodyLenient: the agent accept path drops ids it does not know,
// keeps everything else, and still refuses every other violation.
func TestCompileBodyLenient(t *testing.T) {
	raw := []byte(`{"schema":1,"sections":{"hidden":["cost","page-from-the-future"],"settings_read_only":["guard","section-from-the-future"]}}`)
	if _, _, err := CompileBody(raw, 1<<16); err == nil {
		t.Fatal("the strict compile (publish lint) must still refuse an unknown id")
	}
	spec, canon, err := CompileBodyLenient(raw, 1<<16)
	if err != nil {
		t.Fatalf("lenient compile: %v", err)
	}
	if strings.Join(spec.HiddenSections, ",") != "cost" || strings.Join(spec.ReadOnlySettings, ",") != "guard" {
		t.Fatalf("spec = %+v", spec)
	}
	if strings.Join(spec.IgnoredIDs, ",") != "sections.hidden:page-from-the-future,sections.settings_read_only:section-from-the-future" {
		t.Fatalf("IgnoredIDs = %v", spec.IgnoredIDs)
	}
	var back Body
	if err := json.Unmarshal(canon, &back); err != nil || strings.Contains(string(canon), "future") {
		t.Fatalf("canonical body = %s (%v)", canon, err)
	}
	strictSpec, _, err := CompileBody([]byte(`{"schema":1,"sections":{"hidden":["cost"],"settings_read_only":["guard"]}}`), 1<<16)
	if err != nil || strictSpec.Hash != spec.Hash {
		t.Fatalf("lenient spec hash %s != strict spec hash %s (%v): the applied policy must be exactly the one without the unknown ids", spec.Hash, strictSpec.Hash, err)
	}

	refusals := []struct{ name, body string }{
		{"the T8 floor stays hard", `{"schema":1,"sections":{"hidden":["privacy","page-from-the-future"]}}`},
		{"a duplicate known id stays hard", `{"schema":1,"sections":{"hidden":["cost","cost"]}}`},
		{"hide-and-lock stays hard", `{"schema":1,"sections":{"hidden":["cost"],"read_only":["cost"]}}`},
		{"an unknown top-level field stays hard", `{"schema":1,"sections":{"hidden":["cost"]},"tabs":{}}`},
		{"a newer schema stays hard", `{"schema":99,"sections":{"hidden":["cost"]}}`},
		{"an unknown pin stays hard", `{"schema":2,"pinned":{"no.such.key":true}}`},
	}
	for _, tc := range refusals {
		if _, _, err := CompileBodyLenient([]byte(tc.body), 1<<16); err == nil {
			t.Errorf("%s: lenient compile accepted %s", tc.name, tc.body)
		}
	}
}

func TestIsVocabProjection(t *testing.T) {
	full := Body{Schema: 1, Sections: SectionsV1{Hidden: []string{"agent-config", "cost", "projects"}}}
	held3, _ := ProjectToVocab(full, 3)
	held2, _ := ProjectToVocab(full, 2)
	cases := []struct {
		name string
		held Body
		want bool
	}{
		{"projected for a level-3 node", held3, true},
		{"projected for a level-2 node", held2, true},
		{"the full body itself is not a projection", full, false},
		{"a different policy is not a projection", Body{Schema: 1, Sections: SectionsV1{Hidden: []string{"routing"}}}, false},
		{"a projection plus an extra id is not one", Body{Schema: 1, Sections: SectionsV1{Hidden: []string{"cost", "projects", "routing"}}}, false},
	}
	for _, tc := range cases {
		if got := IsVocabProjection(full, tc.held); got != tc.want {
			t.Errorf("%s: IsVocabProjection = %v, want %v", tc.name, got, tc.want)
		}
	}
}
