package nodegov

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/policyfam/nodegov/navtabsrc"
)

// Per-tab visibility (backlog item 4, T-132): a page tab is addressed in the
// existing nav section lists as "<page>/<tab>". These tests pin the vocabulary
// to the SPA's own tab tables, the compile grammar, and the mixed-fleet rail.

const (
	tabRoutesSourceRel = "../../../web/src/lib/pageTabRoutes.ts"
	tabTablesSourceRel = "../../../web/src/lib/nodePageTabs.ts"
)

// TestNavTabsMatchSPASource fails when navtabs_gen.go is not what the
// generator writes from today's SPA tab tables.
func TestNavTabsMatchSPASource(t *testing.T) {
	routes, err := os.ReadFile(filepath.Clean(tabRoutesSourceRel))
	if err != nil {
		t.Fatal(err)
	}
	tabs, err := os.ReadFile(filepath.Clean(tabTablesSourceRel))
	if err != nil {
		t.Fatal(err)
	}
	pages, err := navtabsrc.Parse(routes, tabs)
	if err != nil {
		t.Fatalf("parse the SPA tab tables: %v", err)
	}
	want, err := navtabsrc.Render(pages)
	if err != nil {
		t.Fatal(err)
	}
	have, err := os.ReadFile("navtabs_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(have, []byte("\r\n"), []byte("\n")), want) {
		t.Fatalf("navtabs_gen.go is stale: the SPA tab tables changed. Run `go generate ./internal/policyfam/nodegov` and commit the result.")
	}
}

func TestNavTabVocabulary(t *testing.T) {
	cases := []struct {
		id        string
		tab       bool
		page, sub string
	}{
		{"security/prompt-guard", true, "security", "prompt-guard"},
		{"security/overview", true, "security", "overview"},
		{"agent-config/history", true, "agent-config", "history"},
		{"security", false, "", ""},
		{"security/no-such-tab", false, "", ""},
		{"overview/overview", false, "", ""}, // Overview is a page without tabs
		{"security/", false, "", ""},
		{"/rules", false, "", ""},
		{"", false, "", ""},
	}
	for _, tc := range cases {
		if got := IsNavTab(tc.id); got != tc.tab {
			t.Errorf("IsNavTab(%q) = %v, want %v", tc.id, got, tc.tab)
		}
		page, sub, ok := SplitNavTab(tc.id)
		if ok != tc.tab || page != tc.page || sub != tc.sub {
			t.Errorf("SplitNavTab(%q) = %q,%q,%v", tc.id, page, sub, ok)
		}
	}
	if got := NavTabID("security", "rules"); got != "security/rules" {
		t.Fatalf("NavTabID = %q", got)
	}
	for _, p := range NavTabs {
		if !IsNavSection(p.Page) {
			t.Errorf("tabbed page %q is not a nav section", p.Page)
		}
		if IsUnhideableNavSection(p.Page) {
			t.Errorf("tabbed page %q is on the T8 floor; its tabs would need a floor of their own", p.Page)
		}
		if got := NavTabsOf(p.Page); len(got) != len(p.Tabs) {
			t.Errorf("NavTabsOf(%q) = %v", p.Page, got)
		}
	}
	if got := NavTabsOf("overview"); got != nil {
		t.Errorf("NavTabsOf(overview) = %v, want nil (no tabs)", got)
	}
	if got := SectionLabel("security/prompt-guard"); got != "Security › Prompt guard" {
		t.Errorf("SectionLabel(tab) = %q", got)
	}
}

func TestCompileTabGrammar(t *testing.T) {
	ok := []struct {
		name string
		in   PolicyInput
	}{
		{"hide one tab", PolicyInput{HiddenSections: []string{"security/prompt-guard"}}},
		{"hide a tab of a page that is also hidden", PolicyInput{HiddenSections: []string{"security", "security/mcp"}}},
		{"expose a page, hide one of its tabs", PolicyInput{VisibleSections: []string{"security"}, HiddenSections: []string{"security/coverage"}}},
		{"lock a page, hide one of its tabs", PolicyInput{ReadOnlySections: []string{"routing"}, HiddenSections: []string{"routing/shadow"}}},
	}
	for _, tc := range ok {
		spec, err := Compile(tc.in)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if len(spec.HiddenSections) == 0 {
			t.Errorf("%s: no hidden ids compiled", tc.name)
		}
	}
	refused := []struct {
		name string
		in   PolicyInput
		want string
	}{
		{"a tab cannot be read-only", PolicyInput{ReadOnlySections: []string{"security/rules"}}, "can only be hidden"},
		{"a tab cannot be exposed", PolicyInput{VisibleSections: []string{"security/rules"}}, "can only be hidden"},
		{"an unknown tab", PolicyInput{HiddenSections: []string{"security/no-such-tab"}}, "not a known"},
		{"a tab twice", PolicyInput{HiddenSections: []string{"cache/events", "cache/events"}}, "twice"},
	}
	for _, tc := range refused {
		_, err := Compile(tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}

// TestTabsRideTheVocabularyProjection is the D-234 compat rail for tabs: a
// node below the tab level is never served a tab id, and keeps receiving the
// rest of the body.
func TestTabsRideTheVocabularyProjection(t *testing.T) {
	if CurrentVocabLevel < FirstTabVocabLevel {
		t.Fatalf("CurrentVocabLevel %d is below FirstTabVocabLevel %d", CurrentVocabLevel, FirstTabVocabLevel)
	}
	for _, id := range NavTabIDs() {
		if lvl := NavSectionSince(id); lvl < FirstTabVocabLevel || lvl > CurrentVocabLevel {
			t.Errorf("tab %q since=%d, want within [%d, %d]", id, lvl, FirstTabVocabLevel, CurrentVocabLevel)
		}
	}
	full := Body{Schema: 1, Sections: SectionsV1{Hidden: []string{"benchmarks", "security/prompt-guard"}, SettingsHidden: []string{"process"}}}
	for level := FirstVocabLevel; level < FirstTabVocabLevel; level++ {
		got, removed := ProjectToVocab(full, level)
		if strings.Join(got.Sections.Hidden, ",") != "benchmarks" {
			t.Errorf("level %d: hidden = %v, want only the page", level, got.Sections.Hidden)
		}
		if strings.Join(got.Sections.SettingsHidden, ",") != "process" {
			t.Errorf("level %d: settings_hidden = %v, the rest of the body must survive", level, got.Sections.SettingsHidden)
		}
		if len(removed) != 1 || removed[0] != "sections.hidden:security/prompt-guard" {
			t.Errorf("level %d: removed = %v", level, removed)
		}
		raw, err := CanonicalJSON(got)
		if err != nil {
			t.Fatalf("level %d: canonical: %v", level, err)
		}
		if strings.Contains(string(raw), "/") {
			t.Errorf("level %d: projected body still names a tab: %s", level, raw)
		}
	}
	got, removed := ProjectToVocab(full, FirstTabVocabLevel)
	if len(removed) != 0 || len(got.Sections.Hidden) != 2 {
		t.Errorf("tab level: got %v removed %v", got.Sections.Hidden, removed)
	}
	if !IsVocabProjection(full, mustProject(full, FirstTabVocabLevel-1)) {
		t.Error("a node served the stripped body must accept the full body at the same version after it upgrades")
	}
}

func mustProject(b Body, level int) Body {
	out, _ := ProjectToVocab(b, level)
	return out
}

// TestLenientCompileDropsTabsItDoesNotKnow: a node one tab level behind drops
// a tab it lacks and applies everything else.
func TestLenientCompileDropsTabsItDoesNotKnow(t *testing.T) {
	raw := []byte(`{"schema":1,"sections":{"hidden":["benchmarks","security/tab-from-the-future","security/mcp"]}}`)
	spec, _, err := CompileBodyLenient(raw, 1<<16)
	if err != nil {
		t.Fatalf("lenient: %v", err)
	}
	if strings.Join(spec.HiddenSections, ",") != "benchmarks,security/mcp" {
		t.Fatalf("hidden = %v", spec.HiddenSections)
	}
	if strings.Join(spec.IgnoredIDs, ",") != "sections.hidden:security/tab-from-the-future" {
		t.Fatalf("ignored = %v", spec.IgnoredIDs)
	}
}

func TestVocabularyServesTabs(t *testing.T) {
	v := Vocabulary()
	if len(v.NavTabs) != len(NavTabIDs()) {
		t.Fatalf("nav_tabs has %d rows, want %d", len(v.NavTabs), len(NavTabIDs()))
	}
	for _, row := range v.NavTabs {
		if row.ID != NavTabID(row.Page, row.Tab) || row.Label == "" {
			t.Errorf("bad nav_tabs row %+v", row)
		}
	}
	for _, s := range v.NavSections {
		if IsNavTab(s.ID) {
			t.Errorf("nav_sections lists tab %q; tabs belong in nav_tabs", s.ID)
		}
	}
}
