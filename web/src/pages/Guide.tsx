import { Link } from "react-router-dom";
import { PageHeader, Pill } from "@/components/primitives";
import { navIcon } from "@/lib/nav";

// Guide - the single-page demo tour for the Daily Briefing + virtuous-cycle
// workstream. Static by design (zero data, zero polling): it tells the story
// of the six-step loop the briefing surfaces, how to run the seeded demo, and
// exactly which parts of the repo were built upon vs tweaked vs added anew.
// Linked from the Briefing page header and the Analyze nav group.

const CYCLE_STEPS: { n: string; title: string; body: string; where: string }[] = [
  {
    n: "1",
    title: "Sense",
    body: "Every AI turn a harness makes lands as a normalized, scrubbed row: session, model, tokens, cache split, latency, errors. ~29 adapters watch the tools you actually run; the reverse proxy tees Anthropic/OpenAI streams.",
    where: "internal/adapter, internal/watcher, internal/proxy",
  },
  {
    n: "2",
    title: "Learn",
    body: "The data is priced per-turn through the cost engine and aggregated into content-free facts - sessions, turns, spend, cache hit rate, error share, model mix - nothing raw ever leaves the machine by default.",
    where: "internal/briefing (pure composer)",
  },
  {
    n: "3",
    title: "Surface",
    body: "The Daily Briefing turns those facts into one page: a narrative, KPI chips, top sessions with their model mix, and opportunity cards whose savings claims carry their own drill-down math.",
    where: "web/src/pages/Briefing.tsx, /api/briefing*",
  },
  {
    n: "4",
    title: "Act",
    body: "Each card is one click away from a config change - reroute trivial sessions, adopt a routing policy, fix a cache-hostile pattern. Applies are recorded in a versioned CTA ledger.",
    where: "/api/briefing/cta/plan · apply · revert",
  },
  {
    n: "5",
    title: "Measure",
    body: "Realized-vs-estimated matches every apply against routing outcomes measured after it: what you were promised vs what actually landed, graded on the decisions the lever really served.",
    where: "realized_vs_estimated + virtuous_cycle cards",
  },
  {
    n: "6",
    title: "Reinforce",
    body: "Applies compound into a virtuous-cycle trend by week. Open questions (template or LLM) keep the loop running: the briefing is a conversation, not a report.",
    where: "Open Questions · triage · coaching · diagnose",
  },
];

const BUILT_UPON: { title: string; detail: string }[] = [
  {
    title: "The capture backbone",
    detail: "Watcher + fsnotify, adapter registry, store idempotent ingest, scrubbing - untouched upstream code that everything below rides on.",
  },
  {
    title: "The cost engine",
    detail: "internal/intelligence/cost pricing tables + config overrides power the per-turn pricing both the briefing and the analysis headline share.",
  },
  {
    title: "The reverse proxy",
    detail: "internal/proxy is the api_turns producer; the briefing scan consumes its rows with the exact proxy-origin dedup the analysis uses.",
  },
  {
    title: "The dashboard SPA shell",
    detail: "Routing, sidebar, useApi, primitives, theming - the new pages are authored in the same conventions and register in the same nav.",
  },
];

const TWEAKED: { title: string; detail: string }[] = [
  {
    title: "api_turns now carry latency + error detail",
    detail: "TTFT / total response / http_status / error_class / error_message columns feed error-rate, TTFT-p95 and failure-pattern cards.",
  },
  {
    title: "Top sessions carry their model mix + errors",
    detail: "SessionRef gained per-model turns/cost and error counts - surfaced in the UI and sent to the LLM as richer, still content-free, context.",
  },
  {
    title: "Briefing page is configurable",
    detail: "Sections (numbers / opportunities / top sessions / activity) are toggleable and reorderable, persisted per-browser - and the LLM open-questions call is now an opt-in toggle, not a default egress.",
  },
  {
    title: "Facts loaders aggregate more",
    detail: "Node (dashboard) and org (rollup) facts scans both count per-session errors and model turns for the enriched drill-down.",
  },
];

const ADDED_NEW: { title: string; detail: string }[] = [
  {
    title: "The pure briefing composer",
    detail: "internal/briefing - deterministic Compose(Facts) → Briefing with narrative, opportunities, justifications, realized-vs-estimated and virtuous-cycle detectors. No DB, no network, no prompts.",
  },
  {
    title: "The LLM briefing runner",
    detail: "internal/llmrun - narrative, open questions, ask, ask-past, triage, coaching, diagnose over a content-free evidence projection; every call is explicit opt-in with template fallbacks.",
  },
  {
    title: "The CTA loop",
    detail: "/api/briefing/cta/plan · apply · revert + the versioned ledger (configapply) that makes action measurable and revertable.",
  },
  {
    title: "Four new dashboard pages",
    detail: "Briefing, Model Fit, API Explorer, Licenses - plus the LLM-connections card in Settings (web) and the org-side Briefing rollup page (web2).",
  },
  {
    title: "The demo seeder",
    detail: "observer demoseed - deterministic multi-user data: last 5 completed days + next 20 future-dated days so a demo keeps working without re-seeding.",
  },
  {
    title: "Backbone migrations 074-084",
    detail: "api_turns latency/error columns, briefing snapshots, failure contexts, routing realized-delta and the FTS session-summary index.",
  },
];

const BENEFITS: string[] = [
  "Every number is recomputable: cards carry their own drill-down math, so 'why does it say $X?' has an answer built in.",
  "No-network-by-default: LLM surfaces are all opt-in and fall back to deterministic templates; raw content never leaves the machine unless you configure it.",
  "Action is measurable: applies are versioned and revertable, and realized-vs-estimated grades the outcome rather than the promise.",
  "Content-free by construction: prompts and pushes carry numbers, tool names and opaque hashes - never transcripts, paths, or prompt text.",
  "Demoable on a laptop: one command seeds a month of realistic activity across 30 users and 6 projects.",
];

export function GuidePage() {
  return (
    <div className="mx-auto max-w-4xl space-y-8 p-6">
      <PageHeader icon={navIcon("guide")}
        title="Guide"
        sub="The Daily Briefing's six-step virtuous cycle, the demo, and exactly what this build added to the stock observer."
      />

      {/* Hero - the loop */}
      <section>
        <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
          The virtuous cycle
        </h2>
        <p className="mb-4 text-[13px] leading-relaxed text-fg-2">
          Observation alone doesn't save money. The briefing turns captured
          activity into a loop: <span className="text-fg-0">sense</span>,{" "}
          <span className="text-fg-0">learn</span>,{" "}
          <span className="text-fg-0">surface</span>,{" "}
          <span className="text-fg-0">act</span>,{" "}
          <span className="text-fg-0">measure</span>,{" "}
          <span className="text-fg-0">reinforce</span>. Each step is one page or
          one API call away from the next.
        </p>
        <div className="space-y-2">
          {CYCLE_STEPS.map((s) => (
            <div
              key={s.n}
              className="flex items-start gap-3 rounded-3 border border-line-2 bg-bg-2 px-4 py-3"
            >
              <span className="mt-0.5 grid h-6 w-6 shrink-0 place-items-center rounded-pill bg-accent-soft font-mono text-[12px] font-semibold text-accent">
                {s.n}
              </span>
              <div className="min-w-0">
                <div className="text-[13px] font-medium text-fg-1">{s.title}</div>
                <p className="mt-0.5 text-[12px] leading-relaxed text-fg-2">{s.body}</p>
                <div className="mt-1 font-mono text-[10.5px] text-fg-3">{s.where}</div>
              </div>
            </div>
          ))}
        </div>
      </section>

      {/* Demo */}
      <section>
        <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
          Run the demo
        </h2>
        <div className="space-y-2 text-[13px] leading-relaxed text-fg-2">
          <p>
            One command seeds a month of realistic multi-user activity:{" "}
            <code className="rounded-2 bg-bg-2 px-1.5 py-0.5 font-mono text-[12px] text-fg-1">
              observer demoseed
            </code>{" "}
            writes the last 5 completed days (ending at yesterday, so the
            briefing's default window is full) plus 20 future-dated days from
            today forward. Re-running purges only previously seeded rows.
          </p>
          <p>
            Then open the{" "}
            <Link to="/briefing" className="font-medium text-accent hover:underline">
              Daily Briefing
            </Link>
            , flip the date picker across the window, and drill into any
            opportunity card - every savings claim carries its math.
          </p>
          <ul className="ml-4 list-disc space-y-1 text-[12.5px] text-fg-3">
            <li>
              <span className="text-fg-2">Routing shadow</span> - yesterday's
              would-have-done under the candidate policy, live on the briefing.
            </li>
            <li>
              <span className="text-fg-2">Realized vs estimated</span> - apply a
              card, then watch the ledger grade the outcome.
            </li>
            <li>
              <span className="text-fg-2">Open questions</span> - toggle LLM
              generation on in the briefing header (off by default; template
              questions otherwise).
            </li>
          </ul>
        </div>
      </section>

      {/* Built upon */}
      <section>
        <h2 className="mb-2 flex items-center gap-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
          Built upon <Pill variant="neutral">untouched upstream</Pill>
        </h2>
        <div className="space-y-2">
          {BUILT_UPON.map((b) => (
            <div key={b.title} className="rounded-3 border border-line-2 bg-bg-2 px-4 py-2.5">
              <div className="text-[13px] font-medium text-fg-1">{b.title}</div>
              <p className="mt-0.5 text-[12px] leading-relaxed text-fg-2">{b.detail}</p>
            </div>
          ))}
        </div>
      </section>

      {/* Tweaked */}
      <section>
        <h2 className="mb-2 flex items-center gap-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
          Tweaked <Pill variant="accent">improved on upstream</Pill>
        </h2>
        <div className="space-y-2">
          {TWEAKED.map((b) => (
            <div key={b.title} className="rounded-3 border border-line-2 bg-bg-2 px-4 py-2.5">
              <div className="text-[13px] font-medium text-fg-1">{b.title}</div>
              <p className="mt-0.5 text-[12px] leading-relaxed text-fg-2">{b.detail}</p>
            </div>
          ))}
        </div>
      </section>

      {/* Added anew */}
      <section>
        <h2 className="mb-2 flex items-center gap-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
          Added anew <Pill variant="warn">new work</Pill>
        </h2>
        <div className="space-y-2">
          {ADDED_NEW.map((b) => (
            <div key={b.title} className="rounded-3 border border-line-2 bg-bg-2 px-4 py-2.5">
              <div className="text-[13px] font-medium text-fg-1">{b.title}</div>
              <p className="mt-0.5 text-[12px] leading-relaxed text-fg-2">{b.detail}</p>
            </div>
          ))}
        </div>
      </section>

      {/* Benefits */}
      <section>
        <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
          Why it matters
        </h2>
        <ul className="space-y-1.5 text-[13px] leading-relaxed text-fg-2">
          {BENEFITS.map((b) => (
            <li key={b} className="flex gap-2">
              <span className="text-accent">·</span>
              <span>{b}</span>
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
