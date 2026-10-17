import { test } from "node:test";
import assert from "node:assert/strict";

import { readFileSync } from "node:fs";
import {
  COMMAND_JOBS,
  COST_HIDDEN_TEXT,
  CANCELABLE_JOBS,
  commandJobCancelPath,
  commandJobPath,
  commandJobPollPath,
  jobElapsed,
  jobOutcome,
  jobSucceeded,
  outputTail,
  redactDollarFigures,
} from "./commandJobs.ts";

test("every job name maps to its own POST route", () => {
  const paths = new Set(COMMAND_JOBS.map(commandJobPath));
  assert.equal(paths.size, COMMAND_JOBS.length);
  assert.equal(commandJobPath("calibration"), "/api/jobs/calibration");
  assert.equal(commandJobPollPath("ab/c"), "/api/jobs/ab%2Fc");
});

test("cancelable jobs name their cancel route (T-421 item 2)", () => {
  assert.deepEqual([...CANCELABLE_JOBS], ["doctor-integrity", "reclaim-cleanup"]);
  for (const name of CANCELABLE_JOBS) assert.ok(COMMAND_JOBS.includes(name));
  assert.equal(commandJobCancelPath("doctor-integrity"), "/api/jobs/doctor-integrity/cancel");
});

test("jobOutcome table", () => {
  const rows: [Parameters<typeof jobOutcome>[0], string | null, string | null][] = [
    [null, null, null],
    [{ status: "running" }, "info", "Running…"],
    [{ status: "done" }, "ok", "Done."],
    [{ status: "failed", error: "backfill exited with code 1" }, "danger", "Failed: backfill exited with code 1"],
    [{ status: "failed" }, "danger", "Failed."],
    [{ status: "canceled", error: "canceled from the dashboard before it finished" }, "info", "Canceled before it finished."],
  ];
  for (const [job, tone, text] of rows) {
    const got = jobOutcome(job);
    assert.equal(got?.tone ?? null, tone);
    assert.equal(got?.text ?? null, text);
  }
  assert.equal(jobOutcome({ status: "done" }, "Scored.")?.text, "Scored.");
});

test("jobElapsed renders the running time", () => {
  const start = "2026-10-05T10:00:00Z";
  const t0 = Date.parse(start);
  assert.equal(jobElapsed(start, t0 + 65_000), "1:05");
  assert.equal(jobElapsed(start, t0 + 3_725_000), "1:02:05");
  assert.equal(jobElapsed(start, t0 - 5_000), "0:00");
  assert.equal(jobElapsed(undefined, t0), "0:00");
  assert.equal(jobElapsed("not a date", t0), "0:00");
});

test("outputTail keeps the last lines", () => {
  assert.equal(outputTail(undefined), "");
  assert.equal(outputTail("a\nb\nc\n", 2), "b\nc");
  assert.equal(outputTail("one line"), "one line");
});

test("jobSucceeded: only a finished, successful job runs success side effects (M4-10)", () => {
  assert.equal(jobSucceeded({ status: "done" }), true);
  assert.equal(jobSucceeded({ status: "failed" }), false);
  assert.equal(jobSucceeded({ status: "running" }), false);
  assert.equal(jobSucceeded({ status: "canceled" }), false);
  assert.equal(jobSucceeded(null), false);
});

test("redactDollarFigures removes every dollar amount the CLI prints (M4-04)", () => {
  const out = redactDollarFigures(
    "estimated matrix cost: ~$0.12 (budget cap $5.00)\ncell a: $0.0042\n$1,234.50 spent, refund -$0.01, 12.50 USD\n3 runs, 1200 tokens",
  );
  assert.equal(/\$|USD/.test(out), false, out);
  assert.match(out, /3 runs, 1200 tokens/);
  assert.equal(out.split(COST_HIDDEN_TEXT).length - 1, 6);
});

test("CommandJobButton: confirm token on start, onDone on success only, dollars redacted in token mode", () => {
  const src = readFileSync(new URL("../components/CommandJobButton.tsx", import.meta.url), "utf8");
  assert.match(src, /headers: await localConfirmHeaders\(\)/);
  assert.match(src, /if \(jobSucceeded\(snap\)\) onDone\?\.\(\)/);
  assert.doesNotMatch(src, /onDone: \(\) => onDone\?\.\(\)/);
  assert.match(src, /dollarsShown\(\) \? rawTail : redactDollarFigures\(rawTail\)/);
});

test("every dashboard write the M3-05 fix covers sends the local confirm headers", () => {
  const files: [string, RegExp][] = [
    ["../pages/Benchmarks.tsx", /\/api\/benchmarks\/(run|delete)/g],
    ["../pages/settings/ConnectedToolsSection.tsx", /\/api\/config\/adopt-defaults/g],
    ["../pages/Compression.tsx", /undo: true/g],
    ["../pages/Settings.tsx", /method: "(POST|PATCH|DELETE)",\n\s+headers: await localConfirmHeaders\(\)/g],
    ["../pages/Requests.tsx", /localConfirmHeaders\(\)/g],
  ];
  for (const [rel, re] of files) {
    const src = readFileSync(new URL(rel, import.meta.url), "utf8");
    assert.ok((src.match(re) ?? []).length > 0, `${rel} lost its write`);
    assert.match(src, /localConfirmHeaders/, `${rel} does not send the confirm token`);
  }
  const settings = readFileSync(new URL("../pages/Settings.tsx", import.meta.url), "utf8");
  assert.equal((settings.match(/\/api\/config\/profiles[^"`]*[`"], undefined, \{\n\s+method: "(POST|PATCH|DELETE)",\n\s+headers: await localConfirmHeaders/g) ?? []).length, 4);
  const requests = readFileSync(new URL("../pages/Requests.tsx", import.meta.url), "utf8");
  assert.equal((requests.match(/localConfirmHeaders\(\)/g) ?? []).length, 3);
});
