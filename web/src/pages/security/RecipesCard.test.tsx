// Component tests of the Security page's guard Recipes card, rendered with
// react-dom/server under node's test runner (scripts/test-components.mjs).
// The pure sentences and builders are pinned in src/lib/guardRecipes.test.ts;
// these prove the card's requests (the preview and replay URLs a selection
// builds, the confirm header every write carries) and its render: problems
// disable Apply, a project scope without the trusted layer is not offered,
// drift and a removal preview read as they should.

import { test } from "node:test";
import assert from "node:assert/strict";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import {
  RecipesView,
  fetchRecipePreview,
  fetchRecipeReplay,
  recipeWrite,
  writeErrorText,
  type RecipesViewProps,
} from "@/pages/security/RecipesCard";
import { ApiError } from "@/lib/api";
import { applyBody, type Recipe, type RecipePreview, type RecipeReplay, type RecipeSelection, type RecipesState } from "@/lib/guardRecipes";

const consoleError = console.error;
console.error = (...args: unknown[]) => {
  if (typeof args[0] === "string" && args[0].includes("useLayoutEffect does nothing on the server")) return;
  consoleError(...args);
};

type Call = { url: string; method: string; headers: Headers; body?: string };

async function withFetch<T>(reply: (url: string) => unknown, run: (calls: Call[]) => Promise<T>): Promise<T> {
  const calls: Call[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    calls.push({ url, method: init?.method ?? "GET", headers: new Headers(init?.headers), body: init?.body as string | undefined });
    return new Response(JSON.stringify(reply(url)), { status: 200, headers: { "Content-Type": "application/json" } });
  }) as typeof fetch;
  try {
    return await run(calls);
  } finally {
    globalThis.fetch = realFetch;
  }
}

function recipe(over: Partial<Recipe>): Recipe {
  return {
    name: "x",
    kind: "pack",
    title: "",
    summary: "",
    version: 1,
    order: 0,
    mode: "",
    prompt_mode: "",
    extends: "",
    advice: [],
    inputs: [],
    source: "builtin",
    overrides: [],
    rule_ids: [],
    ...over,
  };
}

const CATALOG: Recipe[] = [
  recipe({ name: "none", kind: "level", title: "None", order: 0, summary: "Guard off." }),
  recipe({ name: "loose", kind: "level", title: "Loose", order: 10 }),
  recipe({ name: "balanced", kind: "level", title: "Balanced", order: 30 }),
  recipe({ name: "strict", kind: "level", title: "Strict", order: 40, summary: "Balanced plus: reads outside the project ask." }),
  recipe({ name: "lockdown", kind: "level", title: "Lockdown", order: 50 }),
  recipe({ name: "frontend", title: "Frontend & web", summary: "Publishing denies." }),
  recipe({ name: "networking", title: "Networking" }),
  recipe({ name: "team-db", title: "Team database", source: "user" }),
];

function stateOf(over: Partial<RecipesState> = {}): RecipesState {
  return {
    catalog: CATALOG,
    issues: [],
    level: "",
    mode: "observe",
    prompt_mode: "ask-once",
    global: null,
    projects: [],
    level_drift: "",
    trusted_layer: true,
    recipe_dir: "/home/u/.observer/guard-recipes",
    confirm_token: "tok-1",
    ...over,
  };
}

const SEL: RecipeSelection = { level: "strict", packs: ["networking", "frontend"], scope: "global", projectRoot: "" };

function previewOf(over: Partial<RecipePreview> = {}): RecipePreview {
  return {
    scope: "global",
    path: "/home/u/.observer/guard-recipe.toml",
    remove: false,
    level: "strict",
    packs: ["frontend", "networking"],
    text: "# recipe file\n",
    level_before: "",
    level_after: "strict",
    mode_before: "observe",
    mode_after: "enforce",
    prompt_mode_before: "ask-once",
    prompt_mode_after: "block",
    changes: [
      { rule_id: "R-150", doc: "outside", category: "boundary", before: "flag", after: "ask" },
      { rule_id: "R-171", doc: "upload", category: "exfil", before: "ask", after: "deny" },
    ],
    problems: [],
    issues: [],
    advice: ["Run agents in a sandboxed terminal."],
    inputs: ["guard.network.allow_hosts"],
    ...over,
  };
}

function props(over: Partial<RecipesViewProps> = {}): RecipesViewProps {
  const noop = () => {};
  return {
    state: stateOf(),
    loading: false,
    loadError: "",
    projects: [],
    sel: SEL,
    removing: false,
    applied: null,
    onScope: noop,
    onProject: noop,
    onLevel: noop,
    onTogglePack: noop,
    preview: { key: "k", data: previewOf(), loading: false, error: "" },
    replay: null,
    onReplay: noop,
    onApply: noop,
    onStartRemove: noop,
    onCancelRemove: noop,
    onRemove: noop,
    busy: null,
    writeError: "",
    onView: noop,
    onDuplicate: noop,
    onExport: noop,
    onDelete: noop,
    onImport: noop,
    ...over,
  };
}

const render = (p: RecipesViewProps) =>
  renderToStaticMarkup(
    <MemoryRouter>
      <RecipesView {...p} />
    </MemoryRouter>,
  );

// buttonTag returns the opening tag of the element carrying data-testid.
function buttonTag(html: string, id: string): string {
  const m = html.match(new RegExp(`<button[^>]*data-testid="${id}"[^>]*>`));
  assert.ok(m, `no ${id} button`);
  return m[0];
}

test("a selection's preview request names the level, the sorted packs and the scope", async () => {
  await withFetch(
    () => previewOf(),
    async (calls) => {
      await fetchRecipePreview(SEL, false);
      await fetchRecipePreview({ level: "", packs: ["frontend"], scope: "project", projectRoot: "/srv/app" }, false);
      await fetchRecipePreview({ ...SEL, scope: "project", projectRoot: "/srv/app" }, true);
      assert.deepEqual(
        calls.map((c) => [c.method, c.url]),
        [
          ["GET", "/api/guard/recipes/preview?level=strict&packs=frontend%2Cnetworking&scope=global"],
          ["GET", "/api/guard/recipes/preview?packs=frontend&scope=project&project_root=%2Fsrv%2Fapp"],
          ["GET", "/api/guard/recipes/preview?remove=1&scope=project&project_root=%2Fsrv%2Fapp"],
        ],
      );
    },
  );
});

test("the replay asks the simulate route with the recipe and reads recipe_replay", async () => {
  const replay: RecipeReplay = {
    scanned: 10,
    before: { mode: "observe", verdicts: 1, by_decision: { ask: 1 }, by_rule_blocking: {} },
    after: { mode: "enforce", verdicts: 2, by_decision: { ask: 2 }, by_rule_blocking: {} },
  };
  await withFetch(
    (url) => (url.includes("hours=720") ? { window_hours: 720 } : { window_hours: 168, recipe_replay: replay }),
    async (calls) => {
      assert.deepEqual(await fetchRecipeReplay(SEL, 168, false), replay);
      assert.equal(await fetchRecipeReplay(SEL, 720, false), null, "an older daemon without the block");
      assert.equal(calls[0].url, "/api/guard/simulate?hours=168&recipe=1&level=strict&packs=frontend%2Cnetworking&scope=global");
    },
  );
});

test("an apply posts the selection with the confirm token", async () => {
  await withFetch(
    () => ({ applied: true, restart_required: true }),
    async (calls) => {
      const out = await recipeWrite<{ restart_required: boolean }>("/api/guard/recipes/apply", applyBody(SEL), "tok-1");
      assert.equal(out.restart_required, true);
      assert.equal(calls.length, 1);
      assert.equal(calls[0].method, "POST");
      assert.equal(calls[0].url, "/api/guard/recipes/apply");
      assert.equal(calls[0].headers.get("X-Observer-Confirm"), "tok-1");
      assert.equal(calls[0].headers.get("Content-Type"), "application/json");
      assert.deepEqual(JSON.parse(calls[0].body ?? "{}"), { level: "strict", packs: ["frontend", "networking"], scope: "global", project_root: "" });
    },
  );
});

test("a clean preview enables Apply and shows the moves, the changes and the inputs", () => {
  const html = render(props());
  const apply = buttonTag(html, "recipes-apply");
  assert.doesNotMatch(apply, / disabled=""/);
  assert.match(html, /2 rules change/);
  assert.match(html, /Observe/);
  assert.match(html, /Enforce/);
  assert.match(html, /Ask once/);
  assert.match(html, /Block/);
  assert.match(html, /File access outside the project/);
  assert.match(html, /R-171/);
  assert.match(html, /Allowed hosts/);
  assert.match(html, /href="\/settings\?section=guard"/);
  assert.match(html, /Run agents in a sandboxed terminal\./);
  assert.match(html, /Show the file/);
  // Every built-in level stop, then the packs (the user's own marked).
  for (const t of ["None", "Loose", "Balanced", "Strict", "Lockdown"]) assert.match(html, new RegExp(`>${t}</button>`));
  assert.match(html, /Team database \(yours\)/);
  assert.match(html, /aria-pressed="true"[^>]*>Strict</);
});

test("problems refuse the apply and are listed", () => {
  const html = render(props({ preview: { key: "k", data: previewOf({ problems: ["R-160 cannot be loosened"] }), loading: false, error: "" } }));
  assert.match(buttonTag(html, "recipes-apply"), / disabled=""/);
  assert.match(html, /This cannot be applied yet/);
  assert.match(html, /R-160 cannot be loosened/);
  assert.match(html, /Fix the problems above first\./);
});

test("nothing picked: no preview, Apply disabled with the reason", () => {
  const html = render(props({ sel: { ...SEL, level: "", packs: [] }, preview: { key: "", data: null, loading: false, error: "" } }));
  assert.match(html, /Pick a level or at least one pack to see what changes\./);
  assert.match(buttonTag(html, "recipes-apply"), / disabled=""/);
  assert.match(html, /Pick a level or a pack first\./);
  assert.match(html, /No level: the guard mode stays as it is/);
});

test("a project scope says the machine-wide mode cannot move, and needs the trusted layer", () => {
  const proj = render(props({ sel: { ...SEL, scope: "project", projectRoot: "/srv/app" }, preview: { key: "k", data: previewOf({ scope: "project" }), loading: false, error: "" } }));
  assert.match(proj, /A project recipe cannot change the machine-wide mode/);
  const pick = render(props({ sel: { ...SEL, scope: "project", projectRoot: "" } }));
  assert.match(pick, /data-testid="recipes-pick-project"/);
  const off = render(props({ state: stateOf({ trusted_layer: false }) }));
  assert.match(off, /<button[^>]*disabled=""[^>]*title="Project recipes need the trusted per-project rules folder[^"]*"/);
});

test("the applied state, drift and a removal preview", () => {
  const applied = {
    scope: "global" as const,
    path: "/home/u/.observer/guard-recipe.toml",
    readable: true,
    meta: { format: 1, scope: "global" as const, level: { name: "strict", version: 1 }, packs: [{ name: "frontend", version: 1 }], applied_via: "dashboard" },
    updates: ["strict v1 -> v2"],
  };
  const html = render(
    props({
      applied,
      state: stateOf({ global: applied, level_drift: "[guard] level says lockdown but the applied recipe is strict." }),
    }),
  );
  assert.match(html, /Strict v1 \+ Frontend &amp; web v1/);
  assert.match(html, /Newer version available: strict v1 -&gt; v2/);
  assert.match(html, /data-testid="recipes-drift"/);
  assert.match(html, /Remove…/);

  const removing = render(
    props({
      applied,
      removing: true,
      state: stateOf({ global: applied }),
      preview: { key: "r", data: previewOf({ remove: true, level_before: "strict", level_after: "" }), loading: false, error: "" },
    }),
  );
  assert.match(removing, /What removing changes/);
  assert.doesNotMatch(buttonTag(removing, "recipes-remove"), / disabled=""/);
  assert.doesNotMatch(removing, /data-testid="recipes-level"/);
  assert.doesNotMatch(removing, /data-testid="recipes-apply"/);
});

test("no user policy file: a machine-wide apply is refused with the reason; the guard being off is said", () => {
  const html = render(props({ state: stateOf({ user_layer: false, enabled: false }) }));
  assert.match(buttonTag(html, "recipes-apply"), / disabled=""/);
  assert.match(html, /A machine-wide recipe needs your own policy file/);
  assert.match(html, /data-testid="recipes-guard-off"/);
  const project = render(props({ state: stateOf({ user_layer: false }), sel: { ...SEL, scope: "project", projectRoot: "/srv/app" } }));
  assert.doesNotMatch(buttonTag(project, "recipes-apply"), / disabled=""/);
});

test("a refused apply lists the compiled file's problems; other errors read as sent", () => {
  const refused = new ApiError(422, "/api/guard/recipes/apply", '{"applied":false,"problems":["rule R-160: cannot disable an integrity rule"]}');
  assert.equal(writeErrorText(refused), "This recipe did not pass the check:\n- rule R-160: cannot disable an integrity rule");
  assert.equal(writeErrorText(new ApiError(409, "/api/guard/recipes/apply", "unknown project")), "unknown project");
  const html = render(props({ writeError: writeErrorText(refused) }));
  assert.match(html, /data-testid="recipes-write-error"/);
});

test("a replay result reads as one sentence with the top blocking rules", () => {
  const data: RecipeReplay = {
    scanned: 120,
    before: { mode: "enforce", verdicts: 9, by_decision: { ask: 4, deny: 1 }, by_rule_blocking: { "R-150": 4 } },
    after: { mode: "enforce", verdicts: 20, by_decision: { ask: 12, deny: 3 }, by_rule_blocking: { "R-150": 10, "R-171": 3 } },
  };
  const html = render(props({ replay: { key: "k", hours: 168, data, loading: false, error: "" } }));
  assert.match(html, /Over the last 7 days this would have asked 12 times and denied 3 \(today&#x27;s policy: 4 asks, 1 deny\), across 120 actions\./);
  assert.match(html, /With this recipe/);
});
