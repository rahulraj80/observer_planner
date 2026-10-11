// Pins the one section-save flow (configSectionSaveCore.ts, T-432): every
// save is previewed, nothing is written without an explicit yes, and the
// write carries the etag the preview was computed against.
import { test } from "node:test";
import assert from "node:assert/strict";
import { ConfigSaveCancelled, NO_PREVIEW_MESSAGE, runSectionSave, type SectionPreviewRequest } from "./configSectionSaveCore.ts";

type Call = { section: string; params: Record<string, string>; body: string };

function dryRun(over: Record<string, unknown> = {}) {
  return {
    dry_run: true,
    would_write: true,
    changed_keys: ["intelligence.monthly_budget_usd"],
    restart_required: true,
    restart_required_keys: ["intelligence.monthly_budget_usd"],
    applied_live_keys: [],
    next_spawn_keys: [],
    comments_preserved: false,
    write_mode: "reserialize",
    config_path: "/h/.observer/config.toml",
    config_etag: "sha256:abc",
    diff: "+monthly_budget_usd = 75\n",
    diff_stats: { added: 1, removed: 0 },
    needs_confirm: false,
    secret_warnings: [{ key: "intelligence.summary_model", message: "looks like a credential" }],
    ...over,
  };
}

function transport(dry: Record<string, unknown>) {
  const calls: Call[] = [];
  const put = async (section: string, params: Record<string, string>, body: string) => {
    calls.push({ section, params, body });
    return params.dry_run ? dry : { saved: true, section, restart_required: true, write_mode: "reserialize" };
  };
  return { calls, put };
}

test("a save previews, shows the diff and warnings, then writes at the previewed etag", async () => {
  const t = transport(dryRun());
  let shown: SectionPreviewRequest | null = null;
  const res = await runSectionSave("intelligence", { MonthlyBudgetUSD: 75 }, {
    put: t.put,
    prompt: async (req) => {
      shown = req;
      return true;
    },
  });
  assert.equal(res.saved, true);
  assert.deepEqual(
    t.calls.map((c) => c.params),
    [{ dry_run: "1" }, { base_etag: "sha256:abc" }],
  );
  assert.equal(t.calls[0].body, t.calls[1].body, "the write sends exactly the previewed body");
  assert.ok(shown, "the preview dialog was opened");
  const req = shown as unknown as SectionPreviewRequest;
  assert.equal(req.dryRun.diff, "+monthly_budget_usd = 75\n");
  assert.equal(req.dryRun.secret_warnings.length, 1);
  assert.equal(req.section, "intelligence");
});

test("cancel writes nothing and rejects with ConfigSaveCancelled", async () => {
  const t = transport(dryRun());
  await assert.rejects(
    runSectionSave("guard", { Mode: "enforce" }, { put: t.put, prompt: async () => false }),
    (e: unknown) => e instanceof ConfigSaveCancelled && /nothing was written/.test((e as Error).message),
  );
  assert.equal(t.calls.length, 1, "only the dry run was sent");
});

test("no preview dialog fails closed: nothing is written", async () => {
  const t = transport(dryRun());
  await assert.rejects(runSectionSave("routing", {}, { put: t.put, prompt: null }), new RegExp(NO_PREVIEW_MESSAGE.slice(0, 20)));
  assert.equal(t.calls.length, 1);
});

test("a save that would change nothing neither prompts nor writes", async () => {
  const t = transport(dryRun({ would_write: false, secret_warnings: [] }));
  let prompted = false;
  const res = await runSectionSave("process", { Enabled: true }, {
    put: t.put,
    prompt: async () => {
      prompted = true;
      return true;
    },
  });
  assert.equal(res.write_mode, "noop");
  assert.equal(res.restart_required, false);
  assert.equal(prompted, false);
  assert.equal(t.calls.length, 1);
});
