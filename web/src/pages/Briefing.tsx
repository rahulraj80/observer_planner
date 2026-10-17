import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import clsx from "clsx";
import { PageHeader, Pill, StatCard, Toggle } from "@/components/primitives";
import { GearIcon, SparklesIcon } from "@/components/icons";
import { fetchJSON } from "@/lib/api";
import { useApi } from "@/lib/useApi";
import { fmtCompact, fmtInt, fmtPct, fmtUSD } from "@/lib/format";
import {
  type ActionsRailResponse,
  type AppliedLever,
  type BriefingAction,
  type SuspensionsResponse,
  applyCta,
  resumeCta,
  revertCta,
  suspendCta,
} from "@/lib/briefingCta";
import { navIcon } from "@/lib/nav";

// Daily briefing (docs/design/standup-briefing.md Phase A + Stage 2
// "Apply CTA"). Data-only: the server composes the day's cost/usage facts
// into one Briefing JSON with a narrative, KPI chips, per-tool activity,
// top sessions, and opportunity cards whose drill-down justification is
// served from the persisted snapshot (/api/briefing/justify) so page and
// drill-down can never drift.
//
// Stage 2 wiring (this page):
//   - each opportunity card renders an Apply CTA footer (simulate-first
//     config writes with stamped-snapshot undo), plus Pause/Revert once
//     applied and Snooze/Resume for the suspend lifecycle;
//   - an "Actions recommended" rail (/api/briefing/actions) lists the
//     concrete config changes to land today, sorted by projected impact;
//   - a "Suspended recommendations" strip (/api/briefing/suspensions)
//     surfaces snoozed/paused cards with a one-click Resume.
//
// Layout: the narrative and the open questions are pinned above everything
// else; the data sections below them are user-configurable - which to show
// and in what order - persisted per-browser in localStorage
// (`sb_briefing_sections`). Ordering keeps every id present so a hidden
// section keeps its spot in the list.

type BriefingAtGlance = {
  sessions: number;
  turns: number;
  spend_usd: number;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  cache_hit_rate: number;
  error_turns: number;
  active_hours: number;
  fast_share: number;
};

type BriefingActivity = {
  tool: string;
  sessions: number;
  turns: number;
  spend_usd: number;
};

type BriefingSessionRef = {
  id: string;
  tool?: string;
  turns: number;
  cost_usd: number;
};

type RepriceTurn = {
  original_model: string;
  selected_model: string;
  estimated_cost_usd: number;
  reason?: string;
};

type JustificationEvidence = {
  numbers?: Record<string, number>;
  items?: { label: string; value: number; unit?: string }[];
  math?: string;
};

type Justification = {
  claim: string;
  source: string;
  group_by?: string;
  since: string;
  until: string;
  tool?: string;
  project?: string;
  price_basis: string;
  estimate_version: string;
  evidence: JustificationEvidence;
  confidence: number;
  flip_conditions?: string;
  reprice?: RepriceTurn[];
};

type BriefingOpportunity = {
  dedup_key: string;
  detector: string;
  category: string;
  scope: string;
  severity: string;
  title: string;
  nudge: string;
  savings_usd?: number;
  savings_min?: number;
  confidence: number;
  justification: Justification;
};

type Briefing = {
  date: string;
  generated_at: string;
  basis: string;
  window: { since: string; until: string };
  at_glance: BriefingAtGlance;
  activity: BriefingActivity[];
  top_sessions: BriefingSessionRef[];
  opportunities: BriefingOpportunity[];
  narrative: string;
  questions: string[];
};

function severityPill(severity: string) {
  switch (severity) {
    case "suggestion":
      return <Pill variant="accent">suggestion</Pill>;
    case "warning":
      return <Pill variant="warn">warning</Pill>;
    default:
      return <Pill variant="neutral">info</Pill>;
  }
}

function btnCls(accent = false) {
  return accent
    ? "rounded-3 border border-accent/40 bg-accent-soft px-2.5 py-1 text-[11px] font-semibold text-accent hover:bg-accent/10 disabled:cursor-not-allowed disabled:opacity-40"
    : "rounded-3 border border-line-2 bg-bg-1 px-2.5 py-1 text-[11px] font-medium text-fg-1 hover:bg-bg-3 disabled:cursor-not-allowed disabled:opacity-40";
}

// --- Section layout ---------------------------------------------------------

type BriefingSectionId = "numbers" | "actions" | "opportunities" | "top_sessions" | "activity";

const BRIEFING_SECTIONS: { id: BriefingSectionId; label: string; hint: string }[] = [
  { id: "numbers", label: "Numbers", hint: "KPI chips" },
  { id: "actions", label: "Actions", hint: "Recommended config changes, one-click apply" },
  { id: "opportunities", label: "Opportunities", hint: "Detected savings with drill-downs" },
  { id: "top_sessions", label: "Top sessions by spend", hint: "Most expensive sessions" },
  { id: "activity", label: "Activity by tool", hint: "Per-tool sessions, turns, spend" },
];

const DEFAULT_SECTIONS: BriefingSectionId[] = [
  "numbers",
  "actions",
  "opportunities",
  "top_sessions",
  "activity",
];

const SECTIONS_KEY = "sb_briefing_sections";
const QUESTIONS_KEY = "sb_briefing_llm_questions";

type BriefingSectionsState = {
  order: BriefingSectionId[];
  visible: BriefingSectionId[];
};

function isSectionId(id: unknown): id is BriefingSectionId {
  return typeof id === "string" && BRIEFING_SECTIONS.some((s) => s.id === id);
}

function defaultSectionsState(): BriefingSectionsState {
  return { order: [...DEFAULT_SECTIONS], visible: [...DEFAULT_SECTIONS] };
}

function loadSectionsState(): BriefingSectionsState {
  try {
    const raw = localStorage.getItem(SECTIONS_KEY);
    if (!raw) return defaultSectionsState();
    const parsed = JSON.parse(raw) as { order?: unknown; visible?: unknown };
    const order = Array.isArray(parsed.order) ? parsed.order.filter(isSectionId) : [];
    const visible = Array.isArray(parsed.visible) ? parsed.visible.filter(isSectionId) : [];
    for (const id of DEFAULT_SECTIONS) {
      if (!order.includes(id)) order.push(id);
    }
    const visibleSet = new Set(visible);
    return { order, visible: order.filter((id) => visibleSet.has(id)) };
  } catch {
    return defaultSectionsState();
  }
}

function saveSectionsState(s: BriefingSectionsState) {
  try {
    localStorage.setItem(SECTIONS_KEY, JSON.stringify(s));
  } catch {
    /* storage blocked - the layout still applies for this tab */
  }
}

function BriefingSectionsPanel({
  state,
  onUpdate,
  onClose,
}: {
  state: BriefingSectionsState;
  onUpdate: (next: BriefingSectionsState) => void;
  onClose: () => void;
}) {
  const rootRef = useRef<HTMLDivElement>(null);

  // Click-outside + Escape close (mirrors DateRangePopover / ComboChip).
  useEffect(() => {
    function onDown(e: MouseEvent) {
      if (!rootRef.current) return;
      if (!rootRef.current.contains(e.target as Node)) onClose();
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
      }
    }
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  function toggle(id: BriefingSectionId) {
    const visible = state.visible.includes(id)
      ? state.visible.filter((v) => v !== id)
      : [...state.visible, id];
    onUpdate({ ...state, visible });
  }

  function move(id: BriefingSectionId, dir: -1 | 1) {
    const i = state.order.indexOf(id);
    const j = i + dir;
    if (i < 0 || j < 0 || j >= state.order.length) return;
    const order = [...state.order];
    [order[i], order[j]] = [order[j], order[i]];
    onUpdate({ ...state, order });
  }

  return (
    <div
      ref={rootRef}
      className="absolute right-0 top-[calc(100%+4px)] z-50 w-80 overflow-hidden rounded-3 border border-line-2 bg-bg-1 p-3 shadow-drawer"
      role="dialog"
      aria-label="Briefing sections"
    >
      <div className="mb-1.5 flex items-center justify-between">
        <span className="text-[10px] font-semibold uppercase tracking-[0.08em] text-fg-3">
          Sections
        </span>
        <button
          type="button"
          onClick={() => onUpdate(defaultSectionsState())}
          className="text-[11px] font-medium text-accent hover:text-accent-strong"
        >
          Reset to default
        </button>
      </div>
      <ul className="space-y-1">
        {state.order.map((id, idx) => {
          const meta = BRIEFING_SECTIONS.find((s) => s.id === id);
          if (!meta) return null;
          const on = state.visible.includes(id);
          return (
            <li
              key={id}
              className={clsx(
                "flex items-center gap-2 rounded-2 border border-line-1 px-2 py-1.5",
                !on && "opacity-55",
              )}
            >
              <Toggle on={on} onChange={() => toggle(id)} label={meta.label} />
              <span className="min-w-0 flex-1 truncate text-[10.5px] text-fg-3">{meta.hint}</span>
              <div className="flex shrink-0 items-center gap-1">
                <button
                  type="button"
                  aria-label={`Move ${meta.label} up`}
                  disabled={idx === 0}
                  onClick={() => move(id, -1)}
                  className="grid h-6 w-6 place-items-center rounded-2 border border-line-2 bg-bg-2 text-[11px] text-fg-2 hover:bg-bg-3 hover:text-fg-0 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  ↑
                </button>
                <button
                  type="button"
                  aria-label={`Move ${meta.label} down`}
                  disabled={idx === state.order.length - 1}
                  onClick={() => move(id, 1)}
                  className="grid h-6 w-6 place-items-center rounded-2 border border-line-2 bg-bg-2 text-[11px] text-fg-2 hover:bg-bg-3 hover:text-fg-0 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  ↓
                </button>
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

// --- CTA card footer --------------------------------------------------------

// CardCtaFooter renders the per-card action row under an opportunity card:
// Apply buttons per actionable lever, an applied badge + Pause/Revert once
// a lever is live, and a suspended state with Resume. Cards without any
// ledger/rail state render nothing.
function CardCtaFooter({
  o,
  rows,
  applied,
  appliedLevers,
  suspended,
  busy,
  onRun,
}: {
  o: BriefingOpportunity;
  rows: BriefingAction[];
  applied: boolean;
  appliedLevers: AppliedLever[];
  suspended: boolean;
  busy: string | null;
  onRun: (key: string, fn: () => Promise<unknown>) => void;
}) {
  if (rows.length === 0 && !applied && !suspended) return null;

  if (suspended) {
    return (
      <div className="flex items-center justify-between gap-3 border-t border-line-1 px-4 py-2.5">
        <div className="flex items-center gap-2 text-[11px] text-fg-3">
          <Pill variant="warn">suspended</Pill>
          <span>{applied ? "paused" : "snoozed"} - hidden until resumed</span>
        </div>
        <button
          type="button"
          disabled={busy !== null}
          onClick={() => onRun(`resume:${o.dedup_key}`, () => resumeCta(o.dedup_key))}
          className={btnCls()}
        >
          Resume
        </button>
      </div>
    );
  }

  if (applied) {
    return (
      <div className="flex flex-wrap items-center gap-2 border-t border-line-1 px-4 py-2.5">
        <Pill variant="success">applied</Pill>
        {appliedLevers.map((lv) => (
          <span
            key={lv.change_id}
            className="flex items-center gap-1 rounded-2 border border-line-1 bg-bg-1 px-2 py-0.5 font-mono text-[10px] text-fg-2"
          >
            {lv.section}.{lv.key}: {lv.from} → {lv.to}
          </span>
        ))}
        <div className="ml-auto flex items-center gap-1.5">
          <button
            type="button"
            title="Temporarily restore the pre-apply value(s); resume re-applies."
            disabled={busy !== null}
            onClick={() => onRun(`pause:${o.dedup_key}`, () => suspendCta(o.dedup_key, { detector: o.detector, title: o.title }))}
            className={btnCls()}
          >
            Pause
          </button>
          {appliedLevers.map((lv) => (
            <button
              key={lv.change_id}
              type="button"
              title={`Undo ${lv.section}.${lv.key} to ${lv.from}`}
              disabled={busy !== null}
              onClick={() => onRun(`revert:${lv.change_id}`, () => revertCta(lv.section, lv.key))}
              className={btnCls()}
            >
              Revert
            </button>
          ))}
        </div>
      </div>
    );
  }

  const actionable = rows.filter((r) => r.actionable);
  if (actionable.length === 0) return null;

  return (
    <div className="space-y-1 border-t border-line-1 px-4 py-2.5">
      {actionable.map((r) => (
        <div key={r.change_id} className="flex items-center justify-between gap-3">
          <div className="flex min-w-0 flex-wrap items-center gap-2 text-[11px] text-fg-3">
            <span className="font-mono">{r.config_surface}</span>
            <span className="font-mono">{r.from}</span>
            <span className="text-fg-3">→</span>
            <span className="font-mono">{r.to}</span>
            {r.projected_monthly_usd != null && r.projected_monthly_usd > 0 && (
              <span className="text-fg-2">saves {fmtUSD(r.projected_monthly_usd)}/mo</span>
            )}
            {r.restart_required && <Pill variant="warn">restart</Pill>}
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            <button
              type="button"
              disabled={busy !== null}
              onClick={() => onRun(`apply:${r.change_id}`, () => applyCta(o.dedup_key, r.change_id ?? ""))}
              className={btnCls(true)}
            >
              Apply
            </button>
            <button
              type="button"
              title="Mute this recommendation for 7 days (no config change)."
              disabled={busy !== null}
              onClick={() =>
                onRun(`snooze:${o.dedup_key}`, () => suspendCta(o.dedup_key, { detector: o.detector, title: o.title }))
              }
              className={btnCls()}
            >
              Snooze 7d
            </button>
          </div>
        </div>
      ))}
    </div>
  );
}

// --- Page -------------------------------------------------------------------

export function BriefingPage() {
  // Default: server resolves the most recent completed local day. Once the
  // first payload lands, the date picker is seeded so ?date= overrides work.
  const [date, setDate] = useState<string>("");
  // LLM-generated open questions are opt-in (a model round-trip on every
  // load). The choice persists per-browser so it survives app restarts; when
  // off, the server serves the deterministic template questions with zero
  // network egress.
  const [llmQuestions, setLlmQuestions] = useState<boolean>(() => {
    try {
      return localStorage.getItem(QUESTIONS_KEY) === "1";
    } catch {
      return false;
    }
  });
  const api = useApi<Briefing>(
    "/api/briefing",
    { date, questions: llmQuestions ? "llm" : "" },
    [date, llmQuestions],
  );
  // Stage 2 surfaces: the "Actions recommended" rail and the suspended
  // strip. Both refetch on the same date and after every CTA write.
  const actionsApi = useApi<ActionsRailResponse>("/api/briefing/actions", { date }, [date]);
  const suspApi = useApi<SuspensionsResponse>("/api/briefing/suspensions", undefined, []);
  const [openKey, setOpenKey] = useState<string | null>(null);
  const [justify, setJustify] = useState<Justification | null>(null);
  const [sections, setSections] = useState<BriefingSectionsState>(loadSectionsState);
  const [customizing, setCustomizing] = useState(false);
  const [ctaBusy, setCtaBusy] = useState<string | null>(null);
  const [ctaError, setCtaError] = useState<string | null>(null);
  const [ctaNotice, setCtaNotice] = useState<string | null>(null);

  function toggleLLMQuestions(next: boolean) {
    setLlmQuestions(next);
    try {
      localStorage.setItem(QUESTIONS_KEY, next ? "1" : "0");
    } catch {
      /* persistence is best-effort */
    }
  }

  const b = api.data;
  useEffect(() => {
    if (b && !date) setDate(b.date);
  }, [b, date]);

  const spent = useMemo(() => (b ? b.at_glance.spend_usd : 0), [b]);

  // Rail rows keyed by card dedup_key, so each opportunity card can render
  // its own CTA footer without a per-card round-trip.
  const byCard = useMemo(() => {
    const m = new Map<string, BriefingAction[]>();
    for (const a of actionsApi.data?.actions ?? []) {
      const arr = m.get(a.dedup_key) ?? [];
      arr.push(a);
      m.set(a.dedup_key, arr);
    }
    return m;
  }, [actionsApi.data]);

  async function openJustify(key: string) {
    setOpenKey(key);
    setJustify(null);
    try {
      const j = await fetchJSON<Justification>("/api/briefing/justify", {
        id: key,
        date: b?.date,
      });
      setJustify(j);
    } catch {
      setJustify(null);
    }
  }

  function updateSections(next: BriefingSectionsState) {
    setSections(next);
    saveSectionsState(next);
  }

  function onRunResume(dedupKey: string, changeId?: string) {
    runCta(`resume:${dedupKey}:${changeId ?? ""}`, () => resumeCta(dedupKey, changeId));
  }

  // Every CTA write reloads all three surfaces so badges, the suspended
  // strip, and the filtered card set stay in sync. Errors and success
  // notes (restart flags, pause/resume semantics) surface in a banner.
  async function runCta(key: string, fn: () => Promise<unknown>) {
    setCtaBusy(key);
    setCtaError(null);
    setCtaNotice(null);
    try {
      const res = await fn();
      if (res && typeof res === "object") {
        const r = res as { note?: string; ledger_error?: string };
        if (r.note) setCtaNotice(r.note);
        if (r.ledger_error) setCtaError(`Ledger: ${r.ledger_error}`);
      }
      api.reload();
      actionsApi.reload();
      suspApi.reload();
    } catch (e) {
      setCtaError(e instanceof Error ? e.message : String(e));
    } finally {
      setCtaBusy(null);
    }
  }

  if (api.error) {
    return (
      <div className="p-6">
        <PageHeader icon={navIcon("briefing")} title="Daily briefing" sub="Yesterday's cost and usage story." />
        <div className="mt-4 rounded-3 border border-warn/30 bg-warn/5 px-4 py-3 text-[12px] text-warn">
          Briefing unavailable: {api.error.message}
        </div>
      </div>
    );
  }

  if (!b) {
    return (
      <div className="p-6">
        <PageHeader icon={navIcon("briefing")} title="Daily briefing" sub="Yesterday's cost and usage story." />
        <div className="mt-4 text-[12px] text-fg-3">Loading…</div>
      </div>
    );
  }

  const g = b.at_glance;
  const activity = b.activity ?? [];
  const topSessions = b.top_sessions ?? [];
  const opportunities = b.opportunities ?? [];
  const questions = b.questions ?? [];
  const shown = sections.order.filter((id) => sections.visible.includes(id));
  const rail = actionsApi.data;
  const suspensions = suspApi.data?.suspensions ?? [];

  return (
    <div className="space-y-6 p-6">
      <PageHeader icon={navIcon("briefing")}
        title="Daily briefing"
        sub="One page that tells yesterday's story: what ran, what it cost, and what the data says you could change. Data-only - every number is recomputable from the same scan, and every opportunity carries its own drill-down math."
        right={
          <div className="flex items-center gap-2">
            <span
              title="Ask a model for the open-questions list (a network round-trip per load). Off = deterministic template questions."
            >
              <Toggle
                on={llmQuestions}
                onChange={toggleLLMQuestions}
                label="LLM open questions"
              />
            </span>
            <input
              type="date"
              value={date}
              onChange={(e) => setDate(e.target.value)}
              className="rounded-3 border border-line-2 bg-bg-2 px-2 py-1 text-[12px] text-fg-1"
              aria-label="Briefing date"
            />
            <Pill>
              {b.date} · {b.basis} pricing
            </Pill>
            <div className="relative">
              <button
                type="button"
                onClick={() => setCustomizing((c) => !c)}
                aria-expanded={customizing}
                aria-label="Configure briefing sections"
                className="flex items-center gap-1.5 rounded-3 border border-line-2 bg-bg-2 px-2 py-1 text-[12px] text-fg-2 transition-colors hover:bg-bg-3 hover:text-fg-1"
              >
                <GearIcon size={13} />
                Sections
              </button>
              {customizing && (
                <BriefingSectionsPanel
                  state={sections}
                  onUpdate={updateSections}
                  onClose={() => setCustomizing(false)}
                />
              )}
            </div>
            <Link
              to="/guide"
              className="flex items-center gap-1.5 rounded-3 border border-line-2 bg-bg-2 px-2 py-1 text-[12px] text-fg-2 transition-colors hover:bg-bg-3 hover:text-fg-1"
            >
              Guide
            </Link>
          </div>
        }
      />

      {/* Narrative - pinned. */}
      <div className="flex items-start gap-3 rounded-3 border border-line-2 bg-bg-2 px-4 py-3">
        <SparklesIcon size={14} className="mt-0.5 shrink-0 text-accent" />
        <p className="text-[13px] leading-relaxed text-fg-1">{b.narrative}</p>
      </div>

      {/* Open questions - pinned, after the narrative. */}
      {questions.length > 0 && (
        <section>
          <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
            Open questions
          </h2>
          <ul className="space-y-1 text-[12px] text-fg-2">
            {questions.map((q, i) => (
              <li key={i} className="flex gap-2">
                <span className="text-accent">·</span>
                <span>{q}</span>
              </li>
            ))}
          </ul>
        </section>
      )}

      {/* CTA status banner - applies/suspends/resumes land here. */}
      {ctaError && (
        <div className="rounded-3 border border-danger/30 bg-danger-soft px-4 py-2.5 text-[12px] text-danger">
          {ctaError}
        </div>
      )}
      {ctaNotice && (
        <div className="rounded-3 border border-accent/30 bg-accent-soft px-4 py-2.5 text-[12px] text-accent">
          {ctaNotice}
        </div>
      )}

      {/* Configurable sections, in user order. */}
      {shown.map((id) => {
        switch (id) {
          case "numbers":
            return (
              <section key="numbers">
                <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
                  Numbers
                </h2>
                <div className="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-6">
                  <StatCard label="Spend" value={fmtUSD(spent)} unit="USD" accent />
                  <StatCard label="Sessions" value={fmtInt(g.sessions)} />
                  <StatCard label="Turns" value={fmtInt(g.turns)} />
                  <StatCard label="Active hours" value={fmtInt(Math.round(g.active_hours))} />
                  <StatCard label="Cache hit rate" value={fmtPct(g.cache_hit_rate)} />
                  <StatCard label="Error turns" value={fmtInt(g.error_turns)} warn={g.error_turns > 0} />
                  <StatCard label="Input tokens" value={fmtCompact(g.input_tokens)} />
                  <StatCard label="Output tokens" value={fmtCompact(g.output_tokens)} />
                  <StatCard label="Cache read" value={fmtCompact(g.cache_read_tokens)} />
                  <StatCard label="Cache created" value={fmtCompact(g.cache_creation_tokens)} />
                  <StatCard label="Fast turns" value={fmtPct(g.fast_share)} />
                </div>
              </section>
            );
          case "actions":
            return (
              <section key="actions">
                <div className="mb-2 flex items-center justify-between">
                  <h2 className="text-[12px] font-medium uppercase tracking-wide text-fg-3">
                    Actions
                  </h2>
                  {rail && (
                    <span className="text-[10.5px] text-fg-3">
                      {rail.date} · {rail.basis} pricing
                    </span>
                  )}
                </div>

                {/* Suspended strip - snoozed/paused cards with one-click Resume. */}
                {suspensions.length > 0 && (
                  <div className="mb-3 rounded-3 border border-line-2 bg-bg-2 p-3">
                    <h3 className="mb-2 text-[11px] font-medium uppercase tracking-wide text-fg-3">
                      Suspended recommendations
                    </h3>
                    <ul className="space-y-1.5">
                      {suspensions.map((su) => (
                        <li
                          key={`${su.dedup_key}:${su.change_id ?? ""}:${su.kind}`}
                          className="flex items-center justify-between gap-3"
                        >
                          <div className="flex min-w-0 items-center gap-2">
                            <Pill variant={su.kind === "pause" ? "info" : "warn"}>
                              {su.kind}
                            </Pill>
                            <span className="min-w-0 truncate text-[12px] text-fg-1">
                              {su.title || su.dedup_key}
                            </span>
                            {su.until && !su.until.startsWith("0001") && (
                              <span className="shrink-0 text-[10.5px] text-fg-3">
                                until {new Date(su.until).toLocaleDateString()}
                              </span>
                            )}
                            {su.section && su.key && (
                              <span className="shrink-0 font-mono text-[10px] text-fg-3">
                                {su.section}.{su.key}
                              </span>
                            )}
                          </div>
                          <button
                            type="button"
                            disabled={ctaBusy !== null}
                            onClick={() => onRunResume(su.dedup_key, su.change_id)}
                            className={btnCls()}
                          >
                            Resume
                          </button>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}

                {!rail ? (
                  <p className="text-[12px] text-fg-3">Loading actions…</p>
                ) : rail.actions.length === 0 ? (
                  <p className="text-[12px] text-fg-3">
                    Nothing actionable today. Good day.
                  </p>
                ) : (
                  <div className="space-y-1.5">
                    {rail.actions
                      .filter((a) => a.actionable)
                      .map((a) => {
                        const leverApplied = (a.applied_levers ?? []).some(
                          (l) => l.change_id === a.change_id,
                        );
                        const busy = ctaBusy === `apply:${a.change_id}`;
                        return (
                          <div
                            key={a.change_id}
                            className={clsx(
                              "rounded-3 border border-line-2 bg-bg-2 px-3 py-2",
                              leverApplied && "opacity-60",
                            )}
                          >
                            <div className="flex items-center justify-between gap-3">
                              <div className="min-w-0">
                                <div className="flex items-center gap-2">
                                  <span className="text-[12.5px] font-medium text-fg-1">
                                    {a.title}
                                  </span>
                                  {leverApplied && <Pill variant="success">applied</Pill>}
                                  {a.restart_required && <Pill variant="warn">restart</Pill>}
                                </div>
                                <div className="mt-0.5 text-[11px] text-fg-3">
                                  <span className="font-mono">{a.config_surface}</span>:{" "}
                                  <span className="font-mono">{a.from}</span> →{" "}
                                  <span className="font-mono">{a.to}</span>
                                  {a.projected_monthly_usd != null && a.projected_monthly_usd > 0 && (
                                    <span className="text-fg-2">
                                      {" "}
                                      · saves {fmtUSD(a.projected_monthly_usd)}/mo
                                    </span>
                                  )}
                                </div>
                              </div>
                              {leverApplied ? (
                                <span className="shrink-0 text-[11px] text-fg-3">
                                  already applied
                                </span>
                              ) : (
                                <button
                                  type="button"
                                  disabled={busy || ctaBusy !== null}
                                  onClick={() =>
                                    runCta(`apply:${a.change_id}`, () =>
                                      applyCta(a.dedup_key, a.change_id ?? ""),
                                    )
                                  }
                                  className={btnCls(true)}
                                >
                                  {busy ? "Applying…" : "Apply"}
                                </button>
                              )}
                            </div>
                          </div>
                        );
                      })}
                    {rail.actions
                      .filter((a) => !a.actionable)
                      .map((a) => (
                        <div
                          key={a.dedup_key}
                          className="rounded-3 border border-line-1 bg-bg-1 px-3 py-2"
                        >
                          <div className="text-[12px] text-fg-2">{a.title}</div>
                          <div className="text-[11px] text-fg-3">{a.nudge}</div>
                        </div>
                      ))}
                  </div>
                )}
              </section>
            );
          case "activity":
            return (
              <section key="activity">
                <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
                  Activity by tool
                </h2>
                {activity.length === 0 ? (
                  <p className="text-[12px] text-fg-3">No tool activity in this window.</p>
                ) : (
                  <div className="overflow-hidden rounded-3 border border-line-2">
                    <table className="w-full text-left text-[12px]">
                      <thead className="bg-bg-2 text-[11px] uppercase tracking-wide text-fg-3">
                        <tr>
                          <th className="px-3 py-2">Tool</th>
                          <th className="px-3 py-2 text-right">Sessions</th>
                          <th className="px-3 py-2 text-right">Turns</th>
                          <th className="px-3 py-2 text-right">Spend</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-line-1 bg-bg-1">
                        {activity.map((a) => (
                          <tr key={a.tool}>
                            <td className="px-3 py-2 font-medium text-fg-1">{a.tool}</td>
                            <td className="px-3 py-2 text-right text-fg-2">{fmtInt(a.sessions)}</td>
                            <td className="px-3 py-2 text-right text-fg-2">{fmtInt(a.turns)}</td>
                            <td className="px-3 py-2 text-right text-fg-1">{fmtUSD(a.spend_usd)}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </section>
            );
          case "top_sessions":
            return topSessions.length > 0 ? (
              <section key="top_sessions">
                <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
                  Top sessions by spend
                </h2>
                <div className="space-y-1">
                  {topSessions.map((s) => (
                    <div
                      key={s.id}
                      className="flex items-center justify-between rounded-3 border border-line-2 bg-bg-2 px-3 py-2"
                    >
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-[12px] text-fg-1">{s.id}</span>
                        {s.tool ? <Pill>{s.tool}</Pill> : null}
                        <span className="text-[11px] text-fg-3">{fmtInt(s.turns)} turns</span>
                      </div>
                      <div className="flex items-center gap-3">
                        <span className="text-[12px] font-medium text-fg-1">{fmtUSD(s.cost_usd)}</span>
                        <Link
                          to={`/sessions?session=${encodeURIComponent(s.id)}`}
                          className="text-[11px] font-medium text-accent hover:text-accent-strong"
                        >
                          Open →
                        </Link>
                      </div>
                    </div>
                  ))}
                </div>
              </section>
            ) : null;
          case "opportunities":
            return (
              <section key="opportunities">
                <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
                  Opportunities
                </h2>
                {opportunities.length === 0 ? (
                  <p className="text-[12px] text-fg-3">
                    Nothing above the reporting threshold. Good day.
                  </p>
                ) : (
                  <div className="space-y-2">
                    {opportunities.map((o) => {
                      const rows = byCard.get(o.dedup_key) ?? [];
                      const first = rows[0];
                      const applied = first?.applied ?? false;
                      const suspended = first?.suspended ?? false;
                      const appliedLevers = first?.applied_levers ?? [];
                      return (
                        <div key={o.dedup_key} className="rounded-3 border border-line-2 bg-bg-2">
                          <button
                            type="button"
                            onClick={() => openJustify(o.dedup_key)}
                            className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left"
                          >
                            <div className="flex items-center gap-2">
                              {severityPill(o.severity)}
                              <div>
                                <div className="text-[13px] font-medium text-fg-1">{o.title}</div>
                                <div className="text-[11px] text-fg-3">
                                  {o.detector} · {o.scope}
                                </div>
                              </div>
                            </div>
                            <div className="shrink-0 text-right">
                              {o.savings_usd != null && o.savings_usd > 0 && (
                                <div className="text-[13px] font-semibold text-fg-1">
                                  {fmtUSD(o.savings_usd)}
                                </div>
                              )}
                              <div className="text-[11px] text-fg-3">drill down</div>
                            </div>
                          </button>
                          {openKey === o.dedup_key && (
                            <div className="border-t border-line-1 px-4 py-3">
                              {justify ? (
                                <div className="space-y-2 text-[12px] text-fg-2">
                                  <p className="text-fg-1">{justify.claim}</p>
                                  {o.nudge && <p className="text-accent">{o.nudge}</p>}
                                  {justify.reprice && justify.reprice.length > 0 && (
                                    <div className="overflow-hidden rounded-3 border border-line-1">
                                      <table className="w-full text-left text-[11px]">
                                        <thead className="bg-bg-2 uppercase tracking-wide text-fg-3">
                                          <tr>
                                            <th className="px-2 py-1">Original</th>
                                            <th className="px-2 py-1">Replay</th>
                                            <th className="px-2 py-1 text-right">Cost</th>
                                          </tr>
                                        </thead>
                                        <tbody className="divide-y divide-line-1">
                                          {justify.reprice.map((r, i) => (
                                            <tr key={i}>
                                              <td className="px-2 py-1 font-mono">{r.original_model}</td>
                                              <td className="px-2 py-1 font-mono">{r.selected_model}</td>
                                              <td className="px-2 py-1 text-right">
                                                {fmtUSD(r.estimated_cost_usd)}
                                              </td>
                                            </tr>
                                          ))}
                                        </tbody>
                                      </table>
                                    </div>
                                  )}
                                  {justify.evidence.numbers && (
                                    <div className="flex flex-wrap gap-2">
                                      {Object.entries(justify.evidence.numbers).map(([k, v]) => (
                                        <span
                                          key={k}
                                          className="rounded-2 border border-line-1 bg-bg-1 px-2 py-0.5 font-mono text-[10px] text-fg-2"
                                        >
                                          {k}={v}
                                        </span>
                                      ))}
                                    </div>
                                  )}
                                  {justify.evidence.math && (
                                    <p className="font-mono text-[10px] text-fg-3">{justify.evidence.math}</p>
                                  )}
                                  {justify.flip_conditions && (
                                    <p className="text-[11px] italic text-fg-3">
                                      Recheck if: {justify.flip_conditions}
                                    </p>
                                  )}
                                </div>
                              ) : (
                                <p className="text-[11px] text-fg-3">Loading drill-down…</p>
                              )}
                            </div>
                          )}
                          <CardCtaFooter
                            o={o}
                            rows={rows}
                            applied={applied}
                            appliedLevers={appliedLevers}
                            suspended={suspended}
                            busy={ctaBusy}
                            onRun={runCta}
                          />
                        </div>
                      );
                    })}
                  </div>
                )}
              </section>
            );
          default:
            return null;
        }
      })}
    </div>
  );
}
