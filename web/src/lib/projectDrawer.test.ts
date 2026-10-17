import { test } from "node:test";
import assert from "node:assert/strict";
import { projectDrawerFilterNote, windowKey } from "./projectDrawer.ts";

test("windowKey is stable per window value and distinguishes closed ranges", () => {
  assert.equal(windowKey({ days: 30 }), windowKey({ days: 30 }));
  assert.notEqual(windowKey({ days: 30 }), windowKey({ days: 7 }));
  assert.equal(windowKey({ hours: 12 }), "hours=12");
  assert.equal(
    windowKey({ since: "2026-09-01T00:00:00.000Z", until: "2026-09-08T00:00:00.000Z" }),
    "since=2026-09-01T00:00:00.000Z&until=2026-09-08T00:00:00.000Z",
  );
});

test("projectDrawerFilterNote names exactly the set filters the drawer ignores", () => {
  const cases: [string | undefined, string | undefined, string][] = [
    ["all", "all", ""],
    [undefined, "", ""],
    ["codex", "all", "The tool filter does not apply to this drawer: it shows every tool in the project"],
    ["all", "gpt-5.4", "The model filter does not apply to this drawer: it shows every model in the project"],
    ["codex", "gpt-5.4", "Tool and model filters do not apply to this drawer: it shows every tool and model in the project"],
  ];
  for (const [tool, model, want] of cases) assert.equal(projectDrawerFilterNote(tool, model), want);
});
