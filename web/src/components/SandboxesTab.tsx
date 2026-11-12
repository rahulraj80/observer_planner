// SandboxesTab.tsx is the Terminals > Sandboxes tab (the planner fork's
// mockup view 1, 2026-11-11): where a sandboxed run can go, the connected
// SandHold node's advisor inside Observer, and the recent sandboxed runs.
//
// Data, and where it comes from:
//   - This machine: GET /api/terminal/sandbox (the probe the New Terminal
//     dialog reads; what `observer sandbox status` knows), local rows only.
//   - The SandHold node: GET /api/terminal/sandbox/sandhold, the local
//     connection record only (who, which org, what the node offered at
//     connect). Never the key, never a call to the node.
//   - The advisor: GET and POST /api/terminal/sandbox/sandhold/advise, which
//     the daemon answers by calling the node with the key it keeps. They run
//     only when the person presses a button here. The node's words are shown
//     as served, as text.
//   - Recent runs: GET /api/terminal/sandbox/runs, the run history's
//     isolation evidence and the refusal report's counts.
//
// A component of terminals.sandbox (web/compgen): a build without the
// sandbox module has no tab.

import { useState } from "react";
import { Button, ChartShell, CopyOnClick, ErrorState, InlineLoading, Pill } from "@/components/primitives";
import { ApiError, fetchJSON } from "@/lib/api";
import { useApi } from "@/lib/useApi";
import { fmtRelative, fmtShortId } from "@/lib/format";
import { backendState } from "@/lib/sandboxIsolation";
import type { SandboxAvailability, SandboxBackendAvail } from "@/lib/types";
import {
  adviseBody,
  adviseReady,
  durationText,
  findingsText,
  networkWord,
  nodeError,
  placesGroups,
  runIsolationWords,
  statusText,
  tierForClass,
  tierTitle,
  type AdviseAnswers,
  type AdviseQuestion,
  type SandboxRun,
} from "@/lib/sandboxesTab";

/** One isolation the node offered at connect (in its words). */
export type SandholdOffer = { tier: string; title: string; means?: string; networks: string[]; default?: boolean };

/** GET /api/terminal/sandbox/sandhold. */
export type SandholdPlace = {
  connected: boolean;
  node?: string;
  user?: string;
  org?: string;
  expires_at?: string;
  expired?: boolean;
  key_store?: string;
  from_env?: boolean;
  offers: SandholdOffer[];
  targets_error?: string;
  problem?: string;
  component_active: boolean;
  connect_command: string;
  confirm_token: string;
};

/** The node's advice (POST /v1/advise, in its words). */
export type SandholdAdvice = {
  isolation: string;
  isolationMeans?: string;
  network: string;
  runsOn?: string;
  command?: string;
  why?: { about: string; choice: string; because: string }[];
  limits?: string[];
  notes?: string[];
};

export const CONNECT_COMMAND = "observer sandbox connect <node-url>";
const DISCONNECT_COMMAND = "observer sandbox disconnect";

function Chip({ children, title }: { children: React.ReactNode; title?: string }) {
  return (
    <span className="rounded-1 border border-line-2 bg-bg-2 px-2 py-0.5 text-[12px] text-fg-1" title={title}>
      {children}
    </span>
  );
}

function Command({ text }: { text: string }) {
  return (
    <CopyOnClick value={text} title="Copy the command" ariaLabel={`Copy: ${text}`}>
      <code className="block break-all rounded-1 border border-line-1 bg-bg-0 px-2 py-1.5 font-mono text-[12px] text-fg-1">{text}</code>
    </CopyOnClick>
  );
}

function dateText(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
}

// ---------- Places ----------

const STATE_TEXT: Record<string, string> = {
  available: "ready",
  not_enabled: "not enabled",
  needs_module: "module not installed",
  unavailable: "not available",
};

/** LocalPlaces is this machine's backends (not the remote rows). */
export function LocalPlaces({ probe }: { probe: SandboxAvailability | null }) {
  const rows = (probe?.backends ?? []).filter((b: SandboxBackendAvail) => b.location !== "remote" && b.maturity !== "planned");
  if (!probe) return <p className="text-fg-3">The sandbox probe has not answered.</p>;
  if (rows.length === 0) {
    return <p className="text-fg-3">{probe.reason || "No sandbox backend is offered on this machine."}</p>;
  }
  // Only what applies to this machine is listed in the open; rows that wait
  // for a module, and rows for another platform, are folded (demo
  // screenshots 2026-11-12: on Linux they pushed the advisor below the fold).
  const groups = placesGroups(rows, (b) => backendState(b).state);
  return (
    <div className="space-y-2">
      {groups.here.length > 0 ? (
        <BackendList rows={groups.here} label="This machine's sandbox backends" />
      ) : (
        <p className="text-fg-3">No sandbox backend runs here yet.</p>
      )}
      {groups.needsModule.length > 0 && (
        <details data-group="needs-module">
          <summary className="cursor-pointer text-[12px] text-fg-3">Need a module installed ({groups.needsModule.length})</summary>
          <div className="mt-2">
            <BackendList rows={groups.needsModule} label="Backends that need a module installed" />
          </div>
        </details>
      )}
      {groups.other.length > 0 && (
        <details data-group="other-platforms">
          <summary className="cursor-pointer text-[12px] text-fg-3">Other platforms ({groups.other.length})</summary>
          <div className="mt-2">
            <BackendList rows={groups.other} label="Backends for other platforms" />
          </div>
        </details>
      )}
    </div>
  );
}

/** BackendList lists backends with their tier, maturity and state. */
function BackendList({ rows, label }: { rows: SandboxBackendAvail[]; label: string }) {
  return (
    <ul className="space-y-2" aria-label={label}>
      {rows.map((b) => {
        const st = backendState(b);
        const tier = tierForClass(b.class);
        return (
          <li key={b.id} className="space-y-1" data-backend={b.id}>
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="font-medium text-fg-1">{b.label}</span>
              {tier && <Chip title={`Observer class: ${b.class}`}>{tier}</Chip>}
              <Pill variant={b.beta ? "warn" : "neutral"} title={b.verification || undefined}>
                {b.beta ? "beta" : "proven"}
              </Pill>
              <Pill variant={st.state === "available" ? "success" : "neutral"}>{STATE_TEXT[st.state]}</Pill>
            </div>
            {st.state !== "available" && st.reason && <p className="text-[11px] text-fg-3">{st.reason}</p>}
          </li>
        );
      })}
    </ul>
  );
}

/** NodePlace is the connected SandHold node, from the local record. */
export function NodePlace({ place, missing }: { place: SandholdPlace | null; missing?: boolean }) {
  if (missing) return <p className="text-fg-3">SandHold is not part of this build.</p>;
  if (!place) return <p className="text-fg-3">Reading the connection.</p>;
  if (place.problem) return <p className="text-warn">{place.problem}</p>;
  if (!place.connected) {
    return <p className="text-fg-3">No node is connected.</p>;
  }
  const who = place.user ? (
    <>
      signed in as <span className="text-fg-1">{place.user}</span>
      {place.org && (
        <>
          {" "}
          in <span className="text-fg-1">{place.org}</span>
        </>
      )}
    </>
  ) : (
    "the node did not say who the key belongs to"
  );
  return (
    <div className="space-y-2">
      <p className="text-fg-2">
        <span className="break-all font-mono text-fg-1">{place.node}</span>, {who}.
      </p>
      {place.from_env ? (
        <p className="text-[11px] text-fg-3">The key comes from SANDHOLD_API_KEY in the daemon&apos;s environment.</p>
      ) : (
        <p className="text-[11px] text-fg-3">
          {place.key_store ? `Key kept in ${place.key_store}` : "Key kept on this machine"}
          {place.expires_at && (place.expired ? `; it expired on ${dateText(place.expires_at)}, connect again` : `; works until ${dateText(place.expires_at)}`)}.
        </p>
      )}
      {place.offers.length > 0 ? (
        <ul className="flex flex-wrap gap-1.5" aria-label="Isolation the node offers">
          {place.offers.map((o) => (
            <li key={o.tier}>
              <Chip title={o.means}>
                {o.title || tierTitle(o.tier)}
                {o.networks.length > 0 && <span className="text-fg-3"> · network {o.networks.map(networkWord).join(", ")}</span>}
              </Chip>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-[11px] text-fg-3">
          {place.targets_error ? `What it offers could not be read at connect: ${place.targets_error}` : "It offered no isolation at connect."}
        </p>
      )}
      {!place.component_active && (
        <p className="text-[11px] text-warn">
          Runs cannot go there yet: turn on SandHold sandboxes in Settings &gt; Components, then add sandhold to the enabled backends.
        </p>
      )}
      <div className="flex flex-wrap items-center gap-2 text-[11px] text-fg-3">
        {place.node && /^https?:\/\//.test(place.node) && (
          <a className="text-accent hover:underline" href={`${place.node}/app/`} target="_blank" rel="noreferrer noopener">
            Open in SandHold
          </a>
        )}
        <span>Disconnect (revokes the key):</span>
      </div>
      <Command text={DISCONNECT_COMMAND} />
    </div>
  );
}

/** ConnectPlace is "connect another place". */
export function ConnectPlace({ connected }: { connected: boolean }) {
  return (
    <div className="space-y-2">
      <p className="text-fg-2">Run this on this machine, then approve it in the node&apos;s page. There is no key to copy.</p>
      <Command text={CONNECT_COMMAND} />
      <p className="text-[11px] text-fg-3">
        {connected
          ? "Connecting to another node replaces the one above and revokes its key."
          : "Observer keeps the key the node gives and sends it only to that node."}
      </p>
    </div>
  );
}

export function PlacesView({ probe, place, placeMissing }: { probe: SandboxAvailability | null; place: SandholdPlace | null; placeMissing?: boolean }) {
  return (
    <section aria-label="Where runs can go" className="grid gap-3 md:grid-cols-3">
      <ChartShell title="This machine" sub="Sandbox backends here">
        <LocalPlaces probe={probe} />
      </ChartShell>
      <ChartShell title="SandHold node" sub="A node you connected">
        <NodePlace place={place} missing={placeMissing} />
      </ChartShell>
      <ChartShell title="Connect another place" sub="A SandHold node">
        <ConnectPlace connected={!!place?.connected} />
      </ChartShell>
    </section>
  );
}

// ---------- Advisor ----------

function ChoiceButton({ pressed, onClick, children, title }: { pressed: boolean; onClick: () => void; children: React.ReactNode; title?: string }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      title={title}
      onClick={onClick}
      className={
        "rounded-2 border px-2.5 py-1.5 text-left text-[12px] " +
        (pressed ? "border-accent bg-accent-soft text-fg-1" : "border-line-2 text-fg-2 hover:bg-bg-3")
      }
    >
      {children}
    </button>
  );
}

export type AdvisorViewProps = {
  place: SandholdPlace | null;
  questions: AdviseQuestion[] | null;
  answers: AdviseAnswers;
  extraHosts: string;
  advice: SandholdAdvice | null;
  busy: string;
  err: { error: string; fix?: string } | null;
  onAsk: () => void;
  onAnswer: (a: AdviseAnswers) => void;
  onExtraHosts: (v: string) => void;
  onAdvise: () => void;
};

/** AdviceView is the node's advice, in its words. */
export function AdviceView({ advice }: { advice: SandholdAdvice }) {
  return (
    <div className="space-y-2 rounded-2 border border-line-2 bg-bg-1 p-3" data-advice={advice.isolation}>
      <p className="font-medium text-fg-1">
        Advised: {tierTitle(advice.isolation)}, network {networkWord(advice.network)}
        {advice.isolationMeans && <span className="font-normal text-fg-2"> ({advice.isolationMeans})</span>}
      </p>
      {(advice.why ?? []).length > 0 && (
        <ul className="space-y-1 text-[12px]">
          {(advice.why ?? []).map((w) => (
            <li key={w.about}>
              <span className="text-fg-1">{w.choice}.</span> <span className="text-fg-2">{w.because}</span>
            </li>
          ))}
        </ul>
      )}
      {(advice.limits ?? []).length > 0 && (
        <div className="text-[12px]">
          <span className="text-fg-3">What it gives up: </span>
          <span className="text-fg-2">{(advice.limits ?? []).join("; ")}</span>
        </div>
      )}
      {(advice.notes ?? []).map((n) => (
        <p key={n} className="text-[11px] text-fg-3">
          {n}
        </p>
      ))}
      {advice.command && (
        <div className="space-y-1">
          <span className="text-[11px] text-fg-3">The node&apos;s command for it:</span>
          <Command text={advice.command} />
        </div>
      )}
      <p className="text-[11px] text-fg-3">The node gives this advice, so it reads the same in the SandHold portal and its command line.</p>
    </div>
  );
}

export function AdvisorView(p: AdvisorViewProps) {
  const { place, questions, answers, advice, busy, err } = p;
  if (!place?.connected) {
    return (
      <div className="space-y-2">
        <p className="text-fg-2">Connect a SandHold node to ask it which isolation and network fit a run.</p>
        <Command text={place?.connect_command || CONNECT_COMMAND} />
      </div>
    );
  }
  const byId = (id: string) => questions?.find((q) => q.id === id);
  const runs = byId("runs");
  const trust = byId("trust");
  const reach = byId("reach");
  return (
    <div className="space-y-3">
      {!questions && (
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="secondary" onClick={p.onAsk} disabled={busy !== ""}>
            {busy === "ask" ? "Asking the node" : "Ask the node its questions"}
          </Button>
          <span className="text-[11px] text-fg-3">This calls {place.node} with the key Observer keeps. The key never reaches this page.</span>
        </div>
      )}
      {err && (
        <p className="text-[12px] text-warn" role="alert">
          {err.error}
          {err.fix && <span className="text-fg-3"> To fix: {err.fix}</span>}
        </p>
      )}
      {questions && (
        <div className="grid gap-3 lg:grid-cols-3">
          {runs && (
            <fieldset className="space-y-1.5">
              <legend className="mb-1 text-fg-2">1. {runs.ask}</legend>
              <div className="flex flex-wrap gap-1.5">
                {runs.choices.map((c) => (
                  <ChoiceButton key={c.id} pressed={answers.runs === c.id} title={c.means} onClick={() => p.onAnswer({ ...answers, runs: c.id })}>
                    {c.label}
                  </ChoiceButton>
                ))}
              </div>
            </fieldset>
          )}
          {trust && (
            <fieldset className="space-y-1.5">
              <legend className="mb-1 text-fg-2">2. {trust.ask}</legend>
              <div className="flex flex-wrap gap-1.5">
                {trust.choices.map((c) => (
                  <ChoiceButton key={c.id} pressed={answers.trust === c.id} title={c.means} onClick={() => p.onAnswer({ ...answers, trust: c.id })}>
                    {c.label}
                  </ChoiceButton>
                ))}
              </div>
            </fieldset>
          )}
          {reach && (
            <fieldset className="space-y-1.5">
              <legend className="mb-1 text-fg-2">3. {reach.ask}</legend>
              <div className="flex flex-wrap gap-1.5">
                {reach.choices.map((c) => {
                  const on = answers.reach.includes(c.id);
                  return (
                    <ChoiceButton
                      key={c.id}
                      pressed={on}
                      title={(c.hosts ?? []).join(", ") || c.means}
                      onClick={() => p.onAnswer({ ...answers, reach: on ? answers.reach.filter((x) => x !== c.id) : [...answers.reach, c.id] })}
                    >
                      {c.label}
                    </ChoiceButton>
                  );
                })}
              </div>
              <label className="block space-y-1 text-[11px] text-fg-3">
                <span>Other hosts (space or comma separated)</span>
                <input
                  type="text"
                  value={p.extraHosts}
                  onChange={(e) => p.onExtraHosts(e.target.value)}
                  placeholder="api.example.com"
                  className="w-full rounded-1 border border-line-2 bg-bg-0 px-2 py-1 font-mono text-[12px] text-fg-1"
                />
              </label>
            </fieldset>
          )}
        </div>
      )}
      {questions && (
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="primary" onClick={p.onAdvise} disabled={busy !== "" || !adviseReady(answers)}>
            {busy === "advise" ? "Asking the node" : "Get the node's advice"}
          </Button>
          {!adviseReady(answers) && <span className="text-[11px] text-fg-3">Answer the first two questions.</span>}
        </div>
      )}
      {advice && <AdviceView advice={advice} />}
    </div>
  );
}

// ---------- Recent runs ----------

function whereText(r: SandboxRun): string {
  if (r.backend === "sandhold") return "SandHold node";
  return r.backend ? `This machine (${r.backend})` : "This machine";
}

export function RunsView({ runs }: { runs: SandboxRun[] }) {
  if (runs.length === 0) {
    return <p className="text-fg-3">No sandboxed runs yet. Choose an isolation in the New Terminal dialog to start one.</p>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[720px] border-collapse text-[12px]">
        <thead>
          <tr className="text-left text-fg-3">
            {["Run", "Where", "Isolation", "Network", "Status", "Duration", "Findings"].map((h) => (
              <th key={h} scope="col" className="border-b border-line-2 px-2 py-1.5 font-medium">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {runs.map((r) => {
            const found = findingsText(r);
            const iso = runIsolationWords(r);
            return (
              <tr key={r.run_id} data-run={r.run_id} className="border-b border-line-1">
                <td className="px-2 py-1.5">
                  <span className="text-fg-1">{r.tool}</span>{" "}
                  <span className="font-mono text-fg-3" title={r.run_id}>
                    {fmtShortId(r.run_id)}
                  </span>
                  <span className="block text-[11px] text-fg-3">{fmtRelative(r.launched_at)}</span>
                </td>
                <td className="px-2 py-1.5 text-fg-2">{whereText(r)}</td>
                <td className="px-2 py-1.5 text-fg-2" title={iso.title}>
                  {iso.isolation}
                </td>
                <td className="px-2 py-1.5 text-fg-2">{iso.network}</td>
                <td className="px-2 py-1.5 text-fg-2">{statusText(r)}</td>
                <td className="px-2 py-1.5 text-fg-2">{durationText(r.duration_seconds)}</td>
                <td className={"px-2 py-1.5 " + (found ? "text-warn" : "")}>{found}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// ---------- The tab ----------

export function SandboxesTab() {
  const probe = useApi<SandboxAvailability>("/api/terminal/sandbox");
  const place = useApi<SandholdPlace>("/api/terminal/sandbox/sandhold");
  const runs = useApi<{ runs: SandboxRun[] }>("/api/terminal/sandbox/runs", undefined, [], { refreshMs: 30_000 });
  const placeMissing = place.error instanceof ApiError && place.error.status === 404;

  const [questions, setQuestions] = useState<AdviseQuestion[] | null>(null);
  const [answers, setAnswers] = useState<AdviseAnswers>({ runs: "", trust: "", reach: [] });
  const [extraHosts, setExtraHosts] = useState("");
  const [advice, setAdvice] = useState<SandholdAdvice | null>(null);
  const [busy, setBusy] = useState("");
  const [err, setErr] = useState<{ error: string; fix?: string } | null>(null);

  function failed(e: unknown) {
    setErr(e instanceof ApiError ? nodeError(e.body) : { error: e instanceof Error ? e.message : String(e) });
  }

  async function ask() {
    setBusy("ask");
    setErr(null);
    try {
      const r = await fetchJSON<{ questions: AdviseQuestion[] }>("/api/terminal/sandbox/sandhold/advise", undefined, {
        headers: { "X-Observer-Confirm": place.data?.confirm_token ?? "" },
      });
      setQuestions(r.questions ?? []);
    } catch (e) {
      failed(e);
    } finally {
      setBusy("");
    }
  }

  async function adviseNow() {
    if (!place.data?.confirm_token) return;
    setBusy("advise");
    setErr(null);
    setAdvice(null);
    try {
      const r = await fetchJSON<{ advice: SandholdAdvice }>("/api/terminal/sandbox/sandhold/advise", undefined, {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-Observer-Confirm": place.data.confirm_token },
        body: JSON.stringify(adviseBody(answers, extraHosts)),
      });
      setAdvice(r.advice);
    } catch (e) {
      failed(e);
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="space-y-4">
      {probe.loading && !probe.data ? (
        <InlineLoading label="Reading the sandbox backends" />
      ) : (
        <PlacesView probe={probe.data} place={place.data} placeMissing={placeMissing} />
      )}
      {!placeMissing && (
        <ChartShell title="Ask the node" sub="Three questions, answered by the connected SandHold node">
          <AdvisorView
            place={place.data}
            questions={questions}
            answers={answers}
            extraHosts={extraHosts}
            advice={advice}
            busy={busy}
            err={err}
            onAsk={() => void ask()}
            onAnswer={(a) => {
              setAnswers(a);
              setAdvice(null);
            }}
            onExtraHosts={(v) => {
              setExtraHosts(v);
              setAdvice(null);
            }}
            onAdvise={() => void adviseNow()}
          />
        </ChartShell>
      )}
      <ChartShell title="Recent sandboxed runs" sub="What each run was launched in, and what Observer recorded while it ran">
        {runs.error && !runs.data ? (
          <ErrorState title="The run history is not available" error={runs.error} />
        ) : runs.loading && !runs.data ? (
          <InlineLoading label="Reading the run history" />
        ) : (
          <RunsView runs={runs.data?.runs ?? []} />
        )}
      </ChartShell>
    </div>
  );
}
