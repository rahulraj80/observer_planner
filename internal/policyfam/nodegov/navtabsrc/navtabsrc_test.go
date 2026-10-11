package navtabsrc

import (
	"strings"
	"testing"
)

const routesFixture = `
export const NODE_TAB_ROUTES: readonly NodeTabRoute[] = [
  { navId: "security", path: "/security", tabs: SECURITY_TABS },
  { navId: "cache", path: "/cache", tabs: CACHE_TABS },
];
`

const tabsFixture = `
export const SECURITY_TABS = pageTabs(
  ["overview", "prompt-guard"] as const,
  {
    overview: "Overview",
    "prompt-guard": "Prompt guard",
  },
);
export const CACHE_TABS = pageTabs(["overview", "events"] as const, {
  overview: "Overview",
  events: "Events & more",
});
`

func TestParseReadsRoutesInOrder(t *testing.T) {
	pages, err := Parse([]byte(routesFixture), []byte(tabsFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(pages) != 2 || pages[0].NavID != "security" || pages[1].NavID != "cache" {
		t.Fatalf("pages = %+v", pages)
	}
	if got := pages[0].Tabs; len(got) != 2 || got[1] != (Tab{ID: "prompt-guard", Label: "Prompt guard"}) {
		t.Fatalf("security tabs = %+v", got)
	}
	if got := pages[1].Tabs[1].Label; got != "Events & more" {
		t.Fatalf("cache events label = %q", got)
	}
}

func TestParseRefusesPartialTables(t *testing.T) {
	cases := []struct {
		name, routes, tabs, want string
	}{
		{"route names an undeclared table", routesFixture, strings.Replace(tabsFixture, "CACHE_TABS", "OTHER_TABS", 1), "does not declare"},
		{"tab without a label", routesFixture, strings.Replace(tabsFixture, `events: "Events & more",`, "", 1), "has no label"},
		{"label for no tab", routesFixture, strings.Replace(tabsFixture, `events: "Events & more",`, `events: "Events", stray: "Stray",`, 1), "not one of its tab ids"},
		{"duplicate tab", routesFixture, strings.Replace(tabsFixture, `["overview", "events"]`, `["overview", "overview"]`, 1), "twice"},
		{"no routes at all", "export const NODE_TAB_ROUTES = [];", tabsFixture, "parsed no"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.routes), []byte(tc.tabs))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestRenderIsGoSource(t *testing.T) {
	pages, err := Parse([]byte(routesFixture), []byte(tabsFixture))
	if err != nil {
		t.Fatal(err)
	}
	src, err := Render(pages)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(src)
	for _, want := range []string{"DO NOT EDIT", "package nodegov", `{Page: "security", Tabs: []NavTab{`, `{ID: "prompt-guard", Label: "Prompt guard"}`} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered source lacks %q:\n%s", want, s)
		}
	}
}
