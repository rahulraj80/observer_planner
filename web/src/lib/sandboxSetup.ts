// sandboxSetup.ts is the one owner of the sandbox setup panel's copy and
// decisions (sandbox installation lifecycle plan section 3.7), pure functions
// over the wire types in sandboxSetupTypes.ts, so the New Terminal dialog's
// inline panel and the Settings "Set up" / "Manage" panel can never word the
// same fact two ways. The server owns every decision that matters (a step's
// class after the switches, can_run, can_undo, the plan fingerprint); this
// module only phrases it. Dispatch is on a step's class, state and channel
// (its capability shape), never on a backend id.
//
// Copy rules (plan 3.7, the toolInstall.ts honesty convention): never a bare
// "unavailable", name the exact blocker and where the switch lives, never a
// raw `api <status>` string (DI-05), and no em or en dashes.

import type {
  SandboxSetupConsent,
  SandboxSetupPlan,
  SandboxSetupRecord,
  SandboxSetupStep,
  SandboxSetupSummary,
} from "./sandboxSetupTypes.ts";
import { backendState, offeredHere, type BackendStateName } from "./sandboxIsolation.ts";
import { INSTALL_STATUS_COPY, fmtInstallDate, installFailedRemedy } from "./toolUninstall.ts";
import type { SandboxAvailability } from "./types.ts";

// ---------------------------------------------------------------------------
// Sizes and versions
// ---------------------------------------------------------------------------

/** sizeMB renders a byte count in whole MB (decimal, never below 1), as the scanner cards do. */
export function sizeMB(bytes: number): string {
  return `${Math.max(1, Math.round((Number.isFinite(bytes) ? bytes : 0) / 1_000_000))}`;
}

/** versionLabel renders a version with exactly one leading "v" ("54.0" and "v54.0" read "v54.0"). */
export function versionLabel(version: string): string {
  const v = (version || "").trim().replace(/^v/i, "");
  return v ? `v${v}` : "an unknown version";
}

/** shortDigest is the first 12 hex characters of a sha256, the length people compare by eye. */
export function shortDigest(sha256: string): string {
  return (sha256 || "").trim().toLowerCase().slice(0, 12);
}

// ---------------------------------------------------------------------------
// Button labels (one per class)
// ---------------------------------------------------------------------------

/** The primary button's label per class; never has none (words and a docs link only). */
export const CLASS_BUTTON_LABELS: Record<string, string | null> = {
  auto: "Turn on",
  terminal: "Install in terminal",
  elevated: "Install in terminal (asks for your password)",
  guide: "Copy command",
  never: null,
};

/**
 * KIND_TERMINAL_VERBS name what a terminal or elevated step does by its kind,
 * so a step that starts a service or pulls an image never says "Install". A
 * kind absent here installs something (a program, a package).
 */
export const KIND_TERMINAL_VERBS: Record<string, string> = {
  service: "Start in terminal",
  image: "Pull in terminal",
  account: "Sign in in terminal",
  consent: "Open in terminal",
  os_feature: "Turn on in terminal",
};

/**
 * downloadButtonLabel is a pinned download's button: the size and version
 * the person agrees to before the click (the sha256 sits in the consent line
 * beside it).
 */
export function downloadButtonLabel(c: SandboxSetupConsent): string {
  return `Download and install (${sizeMB(c.bytes)} MB, ${versionLabel(c.version)})`;
}

/**
 * stepButtonLabel is the one button a step offers, or null for none. A step
 * that carries a download consent and runs in-process reads as a download;
 * otherwise the label follows the class. A guide step offers "Copy command"
 * only when it has something to copy.
 */
export function stepButtonLabel(step: SandboxSetupStep): string | null {
  if (step.consent && step.class === "auto") return downloadButtonLabel(step.consent);
  if (step.class === "guide") return step.display ? CLASS_BUTTON_LABELS.guide : null;
  const verb = KIND_TERMINAL_VERBS[step.kind];
  if (verb && step.class === "terminal") return verb;
  if (verb && step.class === "elevated") return `${verb} (asks for your password)`;
  if (!(step.class in CLASS_BUTTON_LABELS)) return null;
  return CLASS_BUTTON_LABELS[step.class];
}

/**
 * stepButtonRuns reports whether the step's button asks the daemon to run it
 * (true) or only copies its command (false). A runnable button shows only
 * when the server says can_run.
 */
export function stepButtonRuns(step: SandboxSetupStep): boolean {
  return step.class === "auto" || step.class === "terminal" || step.class === "elevated";
}

// ---------------------------------------------------------------------------
// State chips (one per state)
// ---------------------------------------------------------------------------

/** The chip tone a state reads in. */
export type ChipTone = "ok" | "warn" | "bad" | "info" | "muted";

/** STATE_CHIPS names every closed step state (sandboxsetup.StepState). */
export const STATE_CHIPS: Record<string, { label: string; tone: ChipTone }> = {
  done: { label: "Done", tone: "ok" },
  missing: { label: "Needed", tone: "warn" },
  unknown: { label: "Not checked yet", tone: "info" },
  blocked: { label: "Blocked on this machine", tone: "bad" },
  not_applicable: { label: "Not needed here", tone: "muted" },
  in_flight: { label: "In progress", tone: "info" },
  not_allowed: { label: "Not allowed by your organization", tone: "bad" },
};

/** stateChip is a step state's chip; an unknown state names itself plainly. */
export function stateChip(state: string): { label: string; tone: ChipTone } {
  return STATE_CHIPS[state] ?? { label: (state || "unknown").replaceAll("_", " "), tone: "info" };
}

// ---------------------------------------------------------------------------
// Consent line
// ---------------------------------------------------------------------------

/** SOURCE_LABELS names the closed download sources for people. */
export const SOURCE_LABELS: Record<string, string> = {
  vendor_release: "the vendor's release",
  org_mirror: "your organization's mirror",
  from_file: "a file on this machine",
};

/** sourceLabel names a download source. */
export function sourceLabel(source: string): string {
  return SOURCE_LABELS[source] ?? (source ? source.replaceAll("_", " ") : "an unnamed source");
}

/**
 * consentLine is what a person agrees to before a pinned download: the
 * version, the size in MB, the first 12 hex of the sha256 and the source.
 */
export function consentLine(c: SandboxSetupConsent): string {
  return (
    `Downloads ${versionLabel(c.version)} (${sizeMB(c.bytes)} MB) from ${sourceLabel(c.source)}, ` +
    `checked against sha256 ${shortDigest(c.sha256)} before it is installed.`
  );
}

// ---------------------------------------------------------------------------
// Errors (the closed codes of plan 3.6)
// ---------------------------------------------------------------------------

/** ERROR_SENTENCES maps every closed error code of the setup routes to a sentence. */
export const ERROR_SENTENCES: Record<string, string> = {
  plan_changed:
    "This machine changed since the plan was shown. The fresh plan is below: check the step and press its button again.",
  not_allowed_by_org:
    "Your organization does not allow this sandbox backend on this machine, so it cannot be set up here. Ask your organization's admin about its allowed backends.",
  install_disabled:
    "Guided install is switched off. Turn on \"Allow guided install and uninstall\" in Terminals > Settings > Fresh-agent launch policy, or run the step's command yourself in a terminal.",
  elevation_disabled:
    "Steps that ask for your password are switched off. Turn on \"Let a setup step ask for your password\" in Terminals > Settings > Sandbox isolation ([terminal.sandbox.setup].allow_elevated = true in ~/.observer/config.toml) to let the dashboard open them in a terminal, or copy the command and run it yourself.",
  consent_required:
    "The download needs your agreement to the version, size and checksum shown. Read the line above the button and press it again.",
  live_runs:
    "A sandboxed terminal is still running on this backend. Close it first, then try again.",
  step_not_runnable:
    "This step cannot run from here right now. Press Re-check for the current plan.",
  unknown_step: "This step is no longer in the plan. Press Re-check for the current plan.",
  not_in_build:
    "This Observer build cannot set up sandbox backends. Settings > Components lists the builds that include it.",
  terminal_unavailable:
    "The in-dashboard terminal is unavailable on this daemon, so there is nowhere to run the installer. Either [handoff].allow_dashboard_launch is false in ~/.observer/config.toml, or this OS has no PTY backend.",
};

/**
 * ELEVATED_GUIDE_HINT tells the person how a password step becomes a button
 * (operator ruling O1: [terminal.sandbox.setup].allow_elevated defaults to
 * false, so such a step reads as a guide step until they turn the key on).
 */
export const ELEVATED_GUIDE_HINT =
  "This command asks for your password, so the dashboard only shows it. To have the dashboard open it in a terminal where you type the password, turn on \"Let a setup step ask for your password\" in Terminals > Settings > Sandbox isolation ([terminal.sandbox.setup].allow_elevated = true in ~/.observer/config.toml), then press Re-check.";

/**
 * elevatedGuideHint is ELEVATED_GUIDE_HINT for a guide step whose command
 * elevates (sudo, or a Windows RunAs wrapper), else null. The plan's wire
 * shape carries no "degraded from elevated" flag, so the served command is
 * the evidence.
 */
export function elevatedGuideHint(step: SandboxSetupStep): string | null {
  if (step.class !== "guide" || !step.display) return null;
  return /^\s*sudo\s|-Verb\s+RunAs/i.test(step.display) ? ELEVATED_GUIDE_HINT : null;
}

/** STALE_TOKEN_MSG is the plain-text 403 of the confirm token check. */
export const STALE_TOKEN_MSG = "The dashboard's confirm token for this panel is stale. Reload the page and try again.";

/** NO_TOKEN_MSG is said when /api/remote/config hands out no confirm token. */
export const NO_TOKEN_MSG = "No confirm token. Reload the page.";

/** The parsed error body of a setup route: {"error", "code"} and, for plan_changed, the fresh plan. */
export type ParsedSetupError = { code: string; error: string; plan?: SandboxSetupPlan };

/** parseSetupError reads a setup route's JSON error body; a plain-text body yields its text as error. */
export function parseSetupError(body: string): ParsedSetupError {
  try {
    const j = JSON.parse(body) as { code?: unknown; error?: unknown; plan?: unknown };
    if (j && typeof j === "object") {
      const plan = j.plan && typeof j.plan === "object" ? (j.plan as SandboxSetupPlan) : undefined;
      return {
        code: typeof j.code === "string" ? j.code : "",
        error: typeof j.error === "string" ? j.error : "",
        ...(plan ? { plan } : {}),
      };
    }
  } catch {
    // Not JSON: the confirm token check and a proxy answer in plain text.
  }
  return { code: "", error: (body || "").trim() };
}

/**
 * setupErrorSentence turns a failed setup request into one named sentence.
 * The closed code wins; a code-less 403 naming the confirm token reads as a
 * stale token; otherwise the server's own sentence, else a status sentence.
 * Never a raw `api <status>` string.
 */
export function setupErrorSentence(status: number, body: string): string {
  const p = parseSetupError(body);
  if (p.code && ERROR_SENTENCES[p.code]) return ERROR_SENTENCES[p.code];
  if (status === 403 && /confirm token/i.test(p.error)) return STALE_TOKEN_MSG;
  if (status === 501) return ERROR_SENTENCES.not_in_build;
  if (status === 503) return ERROR_SENTENCES.terminal_unavailable;
  if (p.error) return p.error;
  return status > 0 ? `The setup request failed (HTTP ${status}).` : "The setup request could not reach the daemon.";
}

/**
 * setupRoutesAbsent reports an answer that means this node offers no setup
 * at all (the module's routes answer 404 component_not_installed, or 501 not
 * in this build): the panel then renders nothing.
 */
export function setupRoutesAbsent(status: number, body: string): boolean {
  if (status === 501) return true;
  return status === 404 && /component_not_installed/.test(body || "");
}

// ---------------------------------------------------------------------------
// Records and the "may be waiting for input" rule
// ---------------------------------------------------------------------------

/**
 * SETUP_INPUT_WAIT_MS mirrors internal/toolinstall.InputWaitAfter (90 s): an
 * install still running after it may be sitting at a prompt in its terminal.
 */
export const SETUP_INPUT_WAIT_MS = 90_000;

const IN_FLIGHT_STATUSES = new Set(["installing", "uninstalling"]);

/**
 * maybeWaitingForInput reports a record still in flight long enough that it
 * may be waiting at a prompt: the server's own flag, or 90 s since the
 * record's start (installed_at; uninstall_started_at for an uninstall).
 */
export function maybeWaitingForInput(rec: SandboxSetupRecord | undefined, nowMs: number): boolean {
  if (!rec || !IN_FLIGHT_STATUSES.has(rec.status)) return false;
  if (rec.maybe_waiting_for_input) return true;
  const started = rec.status === "uninstalling" ? rec.uninstall_started_at || rec.installed_at : rec.installed_at;
  const t = Date.parse(started || "");
  return Number.isFinite(t) && nowMs - t >= SETUP_INPUT_WAIT_MS;
}

/** The record line and its follow-up under a step. */
export type RecordView = {
  tone: "info" | "ok" | "warn";
  /** "Installed from this dashboard on <date>." and its siblings. */
  line: string;
  /** The next step after a failure or a missing result, else null. */
  remedy: string | null;
};

/**
 * recordView phrases a step's tool_installs record with the guided install's
 * own lifecycle copy (toolUninstall.ts), so a sandbox step and an agent
 * install read alike. A failed step names its exit code and the remedy.
 */
const INSTALLED_PREFIX = "Installed from this dashboard on ";
const LAST_RUN_PREFIX = "Last run from this dashboard on ";

export function recordView(rec: SandboxSetupRecord, stepDone = true): RecordView {
  const row = INSTALL_STATUS_COPY[rec.status];
  const date = fmtInstallDate(rec.installed_at);
  let line = row ? row.line(date) : `Installed from this dashboard on ${date} (status: ${rec.status || "unknown"}).`;
  // A step that is not done now (its result was removed outside Observer)
  // reads its record as history, never "Installed" (D-R4-5).
  if (!stepDone && line.startsWith(INSTALLED_PREFIX)) line = LAST_RUN_PREFIX + line.slice(INSTALLED_PREFIX.length);
  let remedy: string | null = null;
  if (rec.status === "install_failed") remedy = installFailedRemedy(rec.exit_code);
  else if (rec.status === "installed_not_found") {
    remedy = "The step finished, but the dashboard does not detect its result yet. Press Check this machine, or run the step again.";
  } else if (rec.status === "install_interrupted") {
    remedy = "The step stopped before it finished (the daemon restarted). Run it again.";
  }
  return { tone: row ? row.tone : "info", line, remedy };
}

// ---------------------------------------------------------------------------
// Manage mode: Undo, Update, Repair
// ---------------------------------------------------------------------------

/** One Manage action a step offers. */
export type ManageAction = { action: "undo" | "update" | "repair"; label: string };

const PRESENT_STATUSES = new Set(["installed", "installed_not_found", "install_interrupted", "uninstall_failed", "uninstall_interrupted"]);

/**
 * manageActions are the Manage buttons a step offers, in order:
 *   - Update when the step is done, was placed by this dashboard, and the
 *     download it would fetch now differs from the one placed (the pin moved),
 *     or the server's reason says an update is available;
 *   - Repair when this dashboard set the step up and it no longer verifies
 *     (state missing with a record that says it was installed);
 *   - Undo when the server says the step can be undone.
 */
export function manageActions(step: SandboxSetupStep): ManageAction[] {
  const out: ManageAction[] = [];
  const rec = step.record;
  const placed = !!rec && PRESENT_STATUSES.has(rec.status);
  const pinMoved = !!rec?.digest && !!step.consent?.sha256 && rec.digest.toLowerCase() !== step.consent.sha256.toLowerCase();
  if (step.state === "done" && placed && (pinMoved || /update available/i.test(step.reason ?? ""))) {
    out.push({ action: "update", label: step.consent ? `Update to ${versionLabel(step.consent.version)}` : "Update" });
  }
  if (step.state === "missing" && placed) out.push({ action: "repair", label: "Repair" });
  if (step.can_undo) out.push({ action: "undo", label: "Undo" });
  return out;
}

// ---------------------------------------------------------------------------
// The plan's headline
// ---------------------------------------------------------------------------

/** planHeadline is the one line above a plan's checklist. */
export function planHeadline(plan: SandboxSetupPlan): string {
  if (plan.blocked) return `${plan.label} cannot be set up on this machine: ${plan.blocked}`;
  if (plan.ready) return `${plan.label} is set up and runs here.`;
  if (plan.unconfirmed) return `${plan.label} is set up as far as Observer can see. Press Check this machine to confirm it runs here.`;
  const next = plan.next >= 0 ? plan.steps[plan.next] : undefined;
  return next ? `${plan.label} is not set up yet. Next: ${next.label}.` : `${plan.label} is not set up yet.`;
}

// ---------------------------------------------------------------------------
// The dialog's one line: setup candidates
// ---------------------------------------------------------------------------

/** One offered row on this host that is not ready yet. */
export type SetupCandidate = {
  backend: string;
  label: string;
  /** The next step's label, when the daemon names one. */
  next?: string;
  /** The row's setup is beta (the daemon's sentence). */
  beta?: string;
  /** The probe's state for the row ("unknown" when the probe does not list it). */
  state: BackendStateName | "unknown";
};

/** setupCandidates' answer: the rows and the counts the dialog's one line needs. */
export type SetupCandidates = {
  rows: SetupCandidate[];
  /** rows.length: "N available to set up". */
  count: number;
  /** Of those, how many only need enabling, need their module, or need more. */
  notEnabled: number;
  needsModule: number;
  other: number;
};

/**
 * setupCandidates returns the offered rows on this host that are not ready
 * and that setup can move forward: the daemon's summaries (one per offered
 * row on this host) less the ready ones, the ones that can never run here
 * (blocked) and the ones the probe says are not offered on this node. The
 * probe only classifies each row for the counts.
 */
export function setupCandidates(
  probe: SandboxAvailability | null,
  summaries: SandboxSetupSummary[] | null | undefined,
): SetupCandidates {
  const byId = new Map((probe?.backends ?? []).map((b) => [b.id, b]));
  const rows: SetupCandidate[] = [];
  for (const s of summaries ?? []) {
    if (s.ready || s.blocked) continue;
    const b = byId.get(s.backend);
    if (b && !offeredHere(b)) continue;
    const state: SetupCandidate["state"] = b ? backendState(b).state : "unknown";
    if (state === "available") continue;
    rows.push({
      backend: s.backend,
      label: s.label || b?.label || s.backend,
      ...(s.next ? { next: s.next } : {}),
      ...(s.beta ? { beta: s.beta } : {}),
      state,
    });
  }
  return {
    rows,
    count: rows.length,
    notEnabled: rows.filter((r) => r.state === "not_enabled").length,
    needsModule: rows.filter((r) => r.state === "needs_module").length,
    other: rows.filter((r) => r.state !== "not_enabled" && r.state !== "needs_module").length,
  };
}

/**
 * setupLine is the dialog's one line under the Isolation picker; each row
 * whose setup is beta is named after the count with "(beta)".
 */
export function setupLine(count: number, betaLabels: string[] = []): string {
  const line = `Set up a sandbox backend (${count} available to set up)`;
  return betaLabels.length > 0 ? `${line}: ${betaLabels.map(betaLabel).join(", ")}` : line;
}

/** betaLabel is a row's label with "(beta)" after it. */
export function betaLabel(label: string): string {
  return `${label} (beta)`;
}

/**
 * defaultSetupBackend is the row the panel starts on: the preselected one
 * when the daemon lists it, else the first row that is not ready and not
 * blocked, else the first row.
 */
export function defaultSetupBackend(summaries: SandboxSetupSummary[], preselected?: string): string | undefined {
  if (preselected && summaries.some((s) => s.backend === preselected)) return preselected;
  return (summaries.find((s) => !s.ready && !s.blocked) ?? summaries[0])?.backend;
}

/** summaryOptionLabel is a row in the panel's row chooser. */
export function summaryOptionLabel(s: SandboxSetupSummary): string {
  const label = s.beta ? betaLabel(s.label) : s.label;
  if (s.ready) return `${label} (set up)`;
  if (s.unconfirmed) return `${label} (set up, not checked)`;
  if (s.blocked) return `${label} (cannot run here)`;
  return s.next ? `${label} (next: ${s.next})` : `${label} (not set up)`;
}

/**
 * summaryFromPlan is the row chooser's list with the entry for plan's row
 * read from that plan (D-R4-3): after a check the label follows the checked
 * plan, not the passive summary fetched when the panel opened. Rows the plan
 * does not name are kept as they are.
 */
export function summaryFromPlan(rows: SandboxSetupSummary[], plan: SandboxSetupPlan | null): SandboxSetupSummary[] {
  if (!plan) return rows;
  return rows.map((r) => {
    if (r.backend !== plan.backend) return r;
    const next = plan.next >= 0 ? plan.steps[plan.next]?.label : undefined;
    return { ...r, ready: plan.ready, unconfirmed: plan.unconfirmed, blocked: plan.blocked, beta: plan.beta ?? r.beta, next };
  });
}

/** planInFlight reports a plan with a step still running. */
export function planInFlight(plan: SandboxSetupPlan | null): boolean {
  return !!plan?.steps.some((s) => s.state === "in_flight" || s.record?.status === "installing" || s.record?.status === "uninstalling");
}

/** One row of the re-probe table below. */
type ReprobeRule = { name: string; when: (prevInFlight: boolean | null, inFlight: boolean, resumed: boolean) => boolean; reprobe: boolean };

/**
 * REPROBE_RULES decide, top-down, whether a new plan in the setup panel asks
 * the New Terminal dialog to fetch the sandbox probe again (D-R4-2), so the
 * Isolation text follows a step that settled:
 *   - still_running: a step is still in flight: no.
 *   - step_settled: the plan before this one had a step in flight: yes.
 *   - first_after_resume: the first plan after the dialog came back from a
 *     setup terminal: yes (the step may have settled after the dialog's
 *     first probe).
 *   - otherwise: no.
 */
export const REPROBE_RULES: ReprobeRule[] = [
  { name: "still_running", when: (_p, inFlight) => inFlight, reprobe: false },
  { name: "step_settled", when: (p) => p === true, reprobe: true },
  { name: "first_after_resume", when: (p, _i, resumed) => p === null && resumed, reprobe: true },
  { name: "otherwise", when: () => true, reprobe: false },
];

/** setupSettleReprobe walks REPROBE_RULES. prevInFlight is null for the first plan. */
export function setupSettleReprobe(prevInFlight: boolean | null, inFlight: boolean, resumed: boolean): boolean {
  for (const r of REPROBE_RULES) if (r.when(prevInFlight, inFlight, resumed)) return r.reprobe;
  return false;
}

/**
 * summarySetUp reports a row whose button reads "Manage" rather than "Set
 * up": ready, or set up as far as the passive plan can see.
 */
export function summarySetUp(s: SandboxSetupSummary | undefined): boolean {
  return !!s && (s.ready || !!s.unconfirmed);
}

/**
 * UNKNOWN_RUNNABLE_HINT is said under a step Observer can run but that only
 * the check can judge (a service, an image): it gets its button after "Check
 * this machine" shows it is needed.
 */
export const UNKNOWN_RUNNABLE_HINT =
  "Only the check of this machine can tell whether this step is needed. Press Check this machine; if it is, its button appears here.";

/** unknownStepHint is UNKNOWN_RUNNABLE_HINT for such a step, else null. */
export function unknownStepHint(step: SandboxSetupStep): string | null {
  return step.state === "unknown" && stepButtonRuns(step) ? UNKNOWN_RUNNABLE_HINT : null;
}

/** opProgress is an in-process op's fraction done (0 to 1), or null when its size is unknown. */
export function opProgress(done: number, total: number): number | null {
  if (!(total > 0)) return null;
  return Math.min(1, Math.max(0, done / total));
}

/** opProgressLabel is the line beside an op's progress bar. */
export function opProgressLabel(done: number, total: number): string {
  const p = opProgress(done, total);
  if (p === null) return done > 0 ? `${sizeMB(done)} MB downloaded` : "Starting";
  return `${sizeMB(done)} of ${sizeMB(total)} MB (${Math.round(p * 100)}%)`;
}

// ---------------------------------------------------------------------------
// Docs links and "afterwards" notes
// ---------------------------------------------------------------------------

/** DOCS_BASE is where the repository's docs/ folder is published. */
export const DOCS_BASE = "https://github.com/superbasedapp/observer/blob/main/docs/";

/**
 * docsHref turns a step's docs anchor ("sandboxed-terminals.md#enabling-it")
 * into a link; an https link is kept, anything else (empty, another scheme,
 * a path that climbs out of docs/) yields null.
 */
export function docsHref(docs: string | undefined): string | null {
  const d = (docs || "").trim();
  if (!d) return null;
  if (/^https:\/\//i.test(d)) return d;
  if (/^[a-z][a-z0-9+.-]*:/i.test(d) || d.startsWith("/") || d.includes("..")) return null;
  return DOCS_BASE + d;
}

/** AFTER_NOTES says what the person does once a step ran (the closed After vocabulary). */
export const AFTER_NOTES: Record<string, string> = {
  relogin: "Afterwards, sign out and back in so the change applies to your account.",
  reboot: "Afterwards, restart the computer (or WSL) so the change takes effect.",
  engine_restart: "Afterwards, the container engine restarts; containers it runs stop.",
  daemon_restart: "Afterwards, restart Observer so it picks the change up.",
};

/** afterNote is a step's "afterwards" sentence, or null for none. */
export function afterNote(after: string | undefined): string | null {
  if (!after) return null;
  return AFTER_NOTES[after] ?? `Afterwards: ${after.replaceAll("_", " ")}.`;
}
