import { test } from "node:test";
import assert from "node:assert/strict";
import { defaultPageChoice, hiddenPageChoices } from "./pageRequest.ts";
import { pageRequestLink, requestEntryFrom } from "./requestEntry.ts";
import type { DashboardVisibility } from "./visibility.ts";

const pages = [
  { id: "overview", label: "Overview" },
  { id: "security", label: "Security" },
  { id: "cost", label: "Cost" },
  { id: "routing", label: "Routing" },
];
const tabRoutes: { navId: string; tabs: { ids: string[]; labels: Record<string, string> } }[] = [
  { navId: "security", tabs: { ids: ["overview", "rules"], labels: { overview: "Overview", rules: "Rules" } } },
  { navId: "routing", tabs: { ids: ["overview", "shadow"], labels: { overview: "Overview", shadow: "Shadow" } } },
];

function vis(sections: Record<string, string>, tabs: Record<string, string>): DashboardVisibility {
  const entry = (level: string) => ({ level: level as "hidden", source: "org" as const });
  return {
    managed_defaults: true,
    hidden_sections: [],
    read_only_sections: [],
    hidden_settings: [],
    read_only_settings: [],
    sections: Object.fromEntries(Object.entries(sections).map(([k, v]) => [k, entry(v)])),
    tabs: Object.fromEntries(Object.entries(tabs).map(([k, v]) => [k, entry(v)])),
  };
}

test("hiddenPageChoices lists hidden pages and hidden tabs, never read-only or visible ones", () => {
  const v = vis(
    { overview: "visible", security: "hidden", cost: "read_only", routing: "visible" },
    { "security/overview": "hidden", "security/rules": "hidden", "routing/overview": "visible", "routing/shadow": "hidden" },
  );
  assert.deepEqual(hiddenPageChoices(v, pages, tabRoutes), [
    { id: "security", label: "Security page" },
    { id: "security/overview", label: "Security: Overview tab" },
    { id: "security/rules", label: "Security: Rules tab" },
    { id: "routing/shadow", label: "Routing: Shadow tab" },
  ]);
  assert.deepEqual(hiddenPageChoices(null, pages, tabRoutes), []);
});

test("defaultPageChoice keeps the asked page only while it is still hidden", () => {
  const choices = [
    { id: "security", label: "Security page" },
    { id: "routing/shadow", label: "Routing: Shadow tab" },
  ];
  assert.equal(defaultPageChoice(choices, "routing/shadow"), "routing/shadow");
  assert.equal(defaultPageChoice(choices, "cost"), "security");
  assert.equal(defaultPageChoice([], "cost"), "");
});

test("the page request link opens a page-access request on that page", () => {
  assert.equal(pageRequestLink("routing/shadow"), "/requests?new=page&page=routing%2Fshadow");
  const e = requestEntryFrom(new URLSearchParams("new=page&page=routing/shadow"));
  assert.deepEqual(e, { kind: "type", draft: { request_type: "page_access", message: "", payload: { target: "routing/shadow" } } });
  assert.equal(pageRequestLink(""), "/requests?new=page");
});
