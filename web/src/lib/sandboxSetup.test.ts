// Tests of the sandbox setup panel's pure copy and decisions
// (sandboxSetup.ts, sandbox installation lifecycle plan section 3.7 and 10):
// one test per button label, state chip, error code and rule.

import assert from "node:assert/strict";
import test from "node:test";

import {
  CLASS_BUTTON_LABELS,
  KIND_TERMINAL_VERBS,
  UNKNOWN_RUNNABLE_HINT,
  afterNote,
  docsHref,
  ELEVATED_GUIDE_HINT,
  ERROR_SENTENCES,
  SETUP_INPUT_WAIT_MS,
  STALE_TOKEN_MSG,
  STATE_CHIPS,
  consentLine,
  defaultSetupBackend,
  downloadButtonLabel,
  elevatedGuideHint,
  manageActions,
  maybeWaitingForInput,
  opProgress,
  opProgressLabel,
  parseSetupError,
  planHeadline,
  recordView,
  setupCandidates,
  setupErrorSentence,
  setupLine,
  setupRoutesAbsent,
  shortDigest,
  sizeMB,
  stateChip,
  stepButtonLabel,
  stepButtonRuns,
  summaryOptionLabel,
  summarySetUp,
  unknownStepHint,
  versionLabel,
} from "./sandboxSetup.ts";
import type {
  SandboxSetupConsent,
  SandboxSetupPlan,
  SandboxSetupRecord,
  SandboxSetupStep,
  SandboxSetupSummary,
} from "./sandboxSetupTypes.ts";
import type { SandboxAvailability, SandboxBackendAvail } from "./types.ts";

const DASHES = /[\u2013\u2014]/;

const step = (over: Partial<SandboxSetupStep>): SandboxSetupStep => ({
  prereq: "bwrap",
  kind: "program",
  label: "bubblewrap",
  state: "missing",
  class: "terminal",
  channel: "pm",
  can_run: true,
  can_undo: false,
  ...over,
});

const consent: SandboxSetupConsent = {
  pin: "cloud-hypervisor-linux-amd64",
  version: "54.0",
  sha256: "ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
  bytes: 4_812_345,
  source: "vendor_release",
};

const record = (over: Partial<SandboxSetupRecord>): SandboxSetupRecord => ({
  id: 7,
  tool: "cloud-hypervisor",
  channel: "pinned_download",
  command: "download cloud-hypervisor v54.0",
  status: "installed",
  installed_at: "2026-11-06T09:00:00Z",
  can_uninstall: false,
  can_forget: true,
  subject_kind: "sandbox",
  backend: "cloud-hypervisor",
  ...over,
});

// ---------------------------------------------------------------------------
// Button labels, one test per class
// ---------------------------------------------------------------------------

test("label: auto reads Turn on", () => {
  assert.equal(stepButtonLabel(step({ class: "auto", channel: "component_select" })), "Turn on");
});

test("label: terminal reads Install in terminal", () => {
  assert.equal(stepButtonLabel(step({ class: "terminal" })), "Install in terminal");
});

test("label: elevated says it asks for the password", () => {
  assert.equal(stepButtonLabel(step({ class: "elevated" })), "Install in terminal (asks for your password)");
});

test("label: a pinned download names its size and version", () => {
  const s = step({ class: "auto", channel: "pinned_download", consent });
  assert.equal(stepButtonLabel(s), "Download and install (5 MB, v54.0)");
  assert.equal(downloadButtonLabel({ ...consent, version: "v54.0", bytes: 200 }), "Download and install (1 MB, v54.0)");
});

test("label: guide reads Copy command, and nothing without a command", () => {
  assert.equal(stepButtonLabel(step({ class: "guide", display: "sudo apt-get install -y bubblewrap" })), "Copy command");
  assert.equal(stepButtonLabel(step({ class: "guide" })), null);
});

test("label: never offers no button", () => {
  assert.equal(stepButtonLabel(step({ class: "never", display: "enable nested virtualization" })), null);
  assert.equal(CLASS_BUTTON_LABELS.never, null);
});

test("label: an unknown class offers no button", () => {
  assert.equal(stepButtonLabel(step({ class: "teleport" })), null);
});

// D-11: a terminal step that starts a service or pulls an image never says
// "Install"; one row per kind verb.
for (const [kind, verb] of Object.entries(KIND_TERMINAL_VERBS)) {
  test(`label: a ${kind} terminal step reads ${verb}`, () => {
    assert.equal(stepButtonLabel(step({ kind, class: "terminal", channel: "terminal_cmd" })), verb);
    assert.equal(stepButtonLabel(step({ kind, class: "elevated", channel: "terminal_cmd" })), `${verb} (asks for your password)`);
    assert.doesNotMatch(verb, /install/i);
    assert.doesNotMatch(verb, DASHES);
  });
}

test("label: a program terminal step still reads Install in terminal", () => {
  assert.equal(stepButtonLabel(step({ kind: "program", class: "terminal" })), "Install in terminal");
});

test("unconfirmed: the headline asks for the check, the chooser and Manage say set up", () => {
  const plan: SandboxSetupPlan = { backend: "bwrap", label: "Bubblewrap", steps: [], next: -1, ready: false, unconfirmed: true, checked: false, fingerprint: "f" };
  assert.match(planHeadline(plan), /set up as far as Observer can see\. Press Check this machine/);
  assert.equal(summaryOptionLabel({ backend: "bwrap", label: "Bubblewrap", ready: false, unconfirmed: true }), "Bubblewrap (set up, not checked)");
  assert.equal(summarySetUp({ backend: "bwrap", label: "Bubblewrap", ready: false, unconfirmed: true }), true);
  assert.equal(summarySetUp({ backend: "bwrap", label: "Bubblewrap", ready: true }), true);
  assert.equal(summarySetUp({ backend: "bwrap", label: "Bubblewrap", ready: false, next: "x" }), false);
  assert.equal(summarySetUp(undefined), false);
  assert.doesNotMatch(planHeadline(plan), DASHES);
});

test("an unknown step Observer can run says the check decides; others say nothing", () => {
  assert.equal(unknownStepHint(step({ state: "unknown", class: "terminal", can_run: false })), UNKNOWN_RUNNABLE_HINT);
  assert.equal(unknownStepHint(step({ state: "unknown", class: "never", can_run: false })), null);
  assert.equal(unknownStepHint(step({ state: "missing", class: "terminal" })), null);
  assert.doesNotMatch(UNKNOWN_RUNNABLE_HINT, DASHES);
});

test("only auto, terminal and elevated buttons ask the daemon to run", () => {
  for (const [cls, runs] of [["auto", true], ["terminal", true], ["elevated", true], ["guide", false], ["never", false]] as const) {
    assert.equal(stepButtonRuns(step({ class: cls })), runs, cls);
  }
});

test("no button label carries an em or en dash", () => {
  for (const l of Object.values(CLASS_BUTTON_LABELS)) assert.doesNotMatch(l ?? "", DASHES);
});

// ---------------------------------------------------------------------------
// State chips, one test per state
// ---------------------------------------------------------------------------

const chipCases: [string, string, string][] = [
  ["done", "Done", "ok"],
  ["missing", "Needed", "warn"],
  ["unknown", "Not checked yet", "info"],
  ["blocked", "Blocked on this machine", "bad"],
  ["not_applicable", "Not needed here", "muted"],
  ["in_flight", "In progress", "info"],
  ["not_allowed", "Not allowed by your organization", "bad"],
];
for (const [state, label, tone] of chipCases) {
  test(`chip: ${state} reads ${label}`, () => {
    assert.deepEqual(stateChip(state), { label, tone });
    assert.doesNotMatch(label, DASHES);
  });
}

test("chip: every closed state has a row and an unknown state names itself", () => {
  assert.equal(Object.keys(STATE_CHIPS).length, chipCases.length);
  assert.deepEqual(stateChip("half_done"), { label: "half done", tone: "info" });
});

// ---------------------------------------------------------------------------
// Consent line
// ---------------------------------------------------------------------------

test("consent: version, size in MB, the first 12 hex of the sha256 and the source", () => {
  const line = consentLine(consent);
  assert.equal(
    line,
    "Downloads v54.0 (5 MB) from the vendor's release, checked against sha256 abcdef012345 before it is installed.",
  );
  assert.match(consentLine({ ...consent, source: "org_mirror" }), /from your organization's mirror/);
  assert.doesNotMatch(line, DASHES);
});

test("sizes, versions and digests format as people read them", () => {
  assert.equal(sizeMB(0), "1");
  assert.equal(sizeMB(42_400_000), "42");
  assert.equal(versionLabel("V1.2"), "v1.2");
  assert.equal(versionLabel(""), "an unknown version");
  assert.equal(shortDigest("ABCDEF0123456789ff"), "abcdef012345");
});

// ---------------------------------------------------------------------------
// Error codes, one test per closed code
// ---------------------------------------------------------------------------

const codes: [string, RegExp][] = [
  ["plan_changed", /fresh plan is below/],
  ["not_allowed_by_org", /organization does not allow/],
  ["install_disabled", /Allow guided install and uninstall/],
  ["elevation_disabled", /\[terminal\.sandbox\.setup\]\.allow_elevated/],
  ["consent_required", /agreement to the version, size and checksum/],
  ["live_runs", /still running on this backend/],
  ["step_not_runnable", /cannot run from here right now/],
  ["unknown_step", /no longer in the plan/],
  ["not_in_build", /cannot set up sandbox backends/],
  ["terminal_unavailable", /nowhere to run the installer/],
];
for (const [code, want] of codes) {
  test(`error: ${code} maps to its sentence`, () => {
    const s = setupErrorSentence(409, JSON.stringify({ error: `raw ${code}`, code }));
    assert.match(s, want);
    assert.doesNotMatch(s, /^api \d/);
    assert.doesNotMatch(s, DASHES);
  });
}

test("error: every closed code has a sentence", () => {
  assert.equal(Object.keys(ERROR_SENTENCES).length, codes.length);
});

test("error: a code-less confirm token 403 reads as a stale token", () => {
  assert.equal(setupErrorSentence(403, "forbidden: missing or invalid confirm token\n"), STALE_TOKEN_MSG);
});

test("error: an unknown code keeps the server's sentence, else a status sentence", () => {
  assert.equal(setupErrorSentence(500, JSON.stringify({ error: "the engine said no", code: "weird" })), "the engine said no");
  assert.equal(setupErrorSentence(500, ""), "The setup request failed (HTTP 500).");
  assert.equal(setupErrorSentence(0, ""), "The setup request could not reach the daemon.");
});

test("error: a plan_changed body carries the fresh plan", () => {
  const plan = { backend: "bwrap", label: "Bubblewrap", steps: [], next: -1, ready: true, checked: false, fingerprint: "f2" };
  const p = parseSetupError(JSON.stringify({ error: "plan changed", code: "plan_changed", plan }));
  assert.equal(p.code, "plan_changed");
  assert.equal(p.plan?.fingerprint, "f2");
  assert.deepEqual(parseSetupError("plain text"), { code: "", error: "plain text" });
});

test("rule: 404 component_not_installed and 501 hide the panel; other answers do not", () => {
  assert.equal(setupRoutesAbsent(404, '{"error":"component_not_installed"}'), true);
  assert.equal(setupRoutesAbsent(501, ""), true);
  assert.equal(setupRoutesAbsent(404, "no such backend"), false);
  assert.equal(setupRoutesAbsent(500, "component_not_installed"), false);
});

// ---------------------------------------------------------------------------
// The "may be waiting for input" rule
// ---------------------------------------------------------------------------

test("rule: an install running 90 s or more may be waiting for input", () => {
  const start = Date.parse("2026-11-06T09:00:00Z");
  const r = record({ status: "installing" });
  assert.equal(maybeWaitingForInput(r, start + SETUP_INPUT_WAIT_MS - 1), false);
  assert.equal(maybeWaitingForInput(r, start + SETUP_INPUT_WAIT_MS), true);
  assert.equal(SETUP_INPUT_WAIT_MS, 90_000);
});

test("rule: the server's own flag wins, a finished record never waits", () => {
  const start = Date.parse("2026-11-06T09:00:00Z");
  assert.equal(maybeWaitingForInput(record({ status: "installing", maybe_waiting_for_input: true }), start), true);
  assert.equal(maybeWaitingForInput(record({ status: "installed" }), start + 10 * SETUP_INPUT_WAIT_MS), false);
  assert.equal(maybeWaitingForInput(undefined, start), false);
});

test("rule: an uninstall counts from its own start", () => {
  const start = Date.parse("2026-11-06T10:00:00Z");
  const r = record({ status: "uninstalling", uninstall_started_at: "2026-11-06T10:00:00Z" });
  assert.equal(maybeWaitingForInput(r, start + 1000), false);
  assert.equal(maybeWaitingForInput(r, start + SETUP_INPUT_WAIT_MS), true);
});

// ---------------------------------------------------------------------------
// Records
// ---------------------------------------------------------------------------

test("record: installed reads Installed from this dashboard on <date>", () => {
  const v = recordView(record({}));
  assert.match(v.line, /^Installed from this dashboard on /);
  assert.equal(v.tone, "ok");
  assert.equal(v.remedy, null);
});

test("record: a failed step names its exit code and the remedy", () => {
  const v = recordView(record({ status: "install_failed", exit_code: 100 }));
  assert.equal(v.tone, "warn");
  assert.match(v.remedy ?? "", /exited with code 100/);
});

test("record: installed but not detected and interrupted each name the next step", () => {
  assert.match(recordView(record({ status: "installed_not_found" })).remedy ?? "", /Check this machine/);
  assert.match(recordView(record({ status: "install_interrupted" })).remedy ?? "", /Run it again/);
});

// ---------------------------------------------------------------------------
// Manage actions
// ---------------------------------------------------------------------------

test("manage: Undo only where the server says can_undo", () => {
  assert.deepEqual(manageActions(step({ state: "done", can_undo: true })), [{ action: "undo", label: "Undo" }]);
  assert.deepEqual(manageActions(step({ state: "done", can_undo: false })), []);
});

test("manage: Update when the pin moved past the placed file", () => {
  const s = step({ state: "done", class: "auto", channel: "pinned_download", consent, record: record({ digest: "0000" }), can_undo: true });
  assert.deepEqual(manageActions(s), [
    { action: "update", label: "Update to v54.0" },
    { action: "undo", label: "Undo" },
  ]);
  const same = { ...s, record: record({ digest: consent.sha256.toLowerCase() }) };
  assert.deepEqual(manageActions(same).map((a) => a.action), ["undo"]);
});

test("manage: Update when the server's reason says one is available", () => {
  const s = step({ state: "done", record: record({ channel: "pm" }), reason: "update available: v0.12, installed v0.11" });
  assert.deepEqual(manageActions(s), [{ action: "update", label: "Update" }]);
});

test("manage: Repair when a step this dashboard set up no longer verifies", () => {
  assert.deepEqual(manageActions(step({ state: "missing", record: record({}) })), [{ action: "repair", label: "Repair" }]);
  assert.deepEqual(manageActions(step({ state: "missing", record: record({ status: "install_failed" }) })), []);
  assert.deepEqual(manageActions(step({ state: "missing" })), []);
});

// ---------------------------------------------------------------------------
// The elevated guide hint
// ---------------------------------------------------------------------------

test("rule: a guide step that elevates says how it becomes a button", () => {
  assert.equal(elevatedGuideHint(step({ class: "guide", display: "sudo apt-get install -y bubblewrap" })), ELEVATED_GUIDE_HINT);
  assert.equal(
    elevatedGuideHint(step({ class: "guide", display: "powershell -Command Start-Process dism -Verb RunAs" })),
    ELEVATED_GUIDE_HINT,
  );
  assert.equal(elevatedGuideHint(step({ class: "guide", display: "brew install colima" })), null);
  assert.equal(elevatedGuideHint(step({ class: "elevated", display: "sudo apt-get install -y bubblewrap" })), null);
  assert.doesNotMatch(ELEVATED_GUIDE_HINT, DASHES);
});

// ---------------------------------------------------------------------------
// Plans, summaries and candidates
// ---------------------------------------------------------------------------

const plan = (over: Partial<SandboxSetupPlan>): SandboxSetupPlan => ({
  backend: "bwrap",
  label: "Bubblewrap",
  steps: [step({ state: "done", label: "module" }), step({ label: "bubblewrap" })],
  next: 1,
  ready: false,
  checked: false,
  fingerprint: "f1",
  ...over,
});

test("headline: blocked, ready and the next step", () => {
  assert.equal(planHeadline(plan({ blocked: "runs on native Linux only" })), "Bubblewrap cannot be set up on this machine: runs on native Linux only");
  assert.equal(planHeadline(plan({ ready: true, next: -1 })), "Bubblewrap is set up and runs here.");
  assert.equal(planHeadline(plan({})), "Bubblewrap is not set up yet. Next: bubblewrap.");
  assert.equal(planHeadline(plan({ next: -1 })), "Bubblewrap is not set up yet.");
});

const backend = (over: Partial<SandboxBackendAvail>): SandboxBackendAvail => ({
  id: "bwrap",
  label: "Bubblewrap",
  class: "process",
  location: "local",
  maturity: "beta",
  available: true,
  beta: false,
  capture_by_tier: {},
  component: "terminals.sandbox",
  ...over,
});

const probe: SandboxAvailability = {
  available: true,
  default_on: false,
  backends: [
    backend({}),
    backend({ id: "landlock", label: "Landlock only", available: false, verdict: "disabled_by_config", when_enabled_available: true }),
    backend({ id: "oci-runc", label: "Docker or Podman", available: false, verdict: "module_inactive" }),
    backend({ id: "cloud-hypervisor", label: "Cloud Hypervisor", available: false, verdict: "backend_missing", reason: "cloud-hypervisor not found" }),
    backend({ id: "openshell-org", label: "Org OpenShell", available: false, verdict: "org_gateway_not_offered" }),
  ],
};

const summaries: SandboxSetupSummary[] = [
  { backend: "bwrap", label: "Bubblewrap", ready: true },
  { backend: "landlock", label: "Landlock only", ready: false, next: "Turn on Landlock only" },
  { backend: "oci-runc", label: "Docker or Podman", ready: false, next: "Select the module" },
  { backend: "cloud-hypervisor", label: "Cloud Hypervisor", ready: false, next: "cloud-hypervisor" },
  { backend: "openshell", label: "OpenShell", ready: false, blocked: "OpenShell does not run in WSL2" },
  { backend: "openshell-org", label: "Org OpenShell", ready: false },
];

test("candidates: offered rows that are not ready, with counts per state", () => {
  const c = setupCandidates(probe, summaries);
  assert.deepEqual(c.rows.map((r) => r.backend), ["landlock", "oci-runc", "cloud-hypervisor"]);
  assert.equal(c.count, 3);
  assert.equal(c.notEnabled, 1);
  assert.equal(c.needsModule, 1);
  assert.equal(c.other, 1);
  assert.equal(c.rows[0].next, "Turn on Landlock only");
});

test("candidates: no summaries means nothing to set up; no probe keeps the daemon's rows", () => {
  assert.equal(setupCandidates(probe, null).count, 0);
  const c = setupCandidates(null, summaries);
  assert.equal(c.count, 4);
  assert.ok(c.rows.every((r) => r.state === "unknown"));
});

test("the dialog's one line names the count", () => {
  assert.equal(setupLine(3), "Set up a sandbox backend (3 available to set up)");
});

test("the panel starts on the preselected row, else the first not ready", () => {
  assert.equal(defaultSetupBackend(summaries, "oci-runc"), "oci-runc");
  assert.equal(defaultSetupBackend(summaries, "nope"), "landlock");
  assert.equal(defaultSetupBackend([{ backend: "bwrap", label: "Bubblewrap", ready: true }]), "bwrap");
  assert.equal(defaultSetupBackend([]), undefined);
});

test("the row chooser labels each row's state", () => {
  assert.equal(summaryOptionLabel(summaries[0]), "Bubblewrap (set up)");
  assert.equal(summaryOptionLabel(summaries[1]), "Landlock only (next: Turn on Landlock only)");
  assert.equal(summaryOptionLabel(summaries[4]), "OpenShell (cannot run here)");
  assert.equal(summaryOptionLabel(summaries[5]), "Org OpenShell (not set up)");
});

test("op progress: a fraction and a line, unknown size handled", () => {
  assert.equal(opProgress(5, 10), 0.5);
  assert.equal(opProgress(5, 0), null);
  assert.equal(opProgress(20, 10), 1);
  assert.equal(opProgressLabel(2_000_000, 4_000_000), "2 of 4 MB (50%)");
  assert.equal(opProgressLabel(0, 0), "Starting");
  assert.equal(opProgressLabel(3_000_000, 0), "3 MB downloaded");
});

// ---------------------------------------------------------------------------
// Docs links and "afterwards" notes
// ---------------------------------------------------------------------------

test("docs: an anchor becomes a docs link; unsafe ones do not", () => {
  assert.equal(docsHref("sandboxed-terminals.md#enabling-it"), "https://github.com/superbasedapp/observer/blob/main/docs/sandboxed-terminals.md#enabling-it");
  assert.equal(docsHref("https://superbased.app/docs/x"), "https://superbased.app/docs/x");
  for (const bad of ["", undefined, "javascript:alert(1)", "/etc/passwd", "../secret.md", "http://x"]) assert.equal(docsHref(bad), null, String(bad));
});

const afterCases: [string, RegExp][] = [
  ["relogin", /sign out and back in/],
  ["reboot", /restart the computer/],
  ["engine_restart", /container engine restarts/],
  ["daemon_restart", /restart Observer/],
];
for (const [after, want] of afterCases) {
  test(`after: ${after} names what to do`, () => {
    assert.match(afterNote(after) ?? "", want);
    assert.doesNotMatch(afterNote(after) ?? "", DASHES);
  });
}

test("after: nothing for none, an unknown value names itself", () => {
  assert.equal(afterNote(""), null);
  assert.equal(afterNote(undefined), null);
  assert.equal(afterNote("power_cycle"), "Afterwards: power cycle.");
});
