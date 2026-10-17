import { useEffect, useMemo, useState } from "react";
import { History, Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import {
  Button,
  Callout,
  Card,
  EmptyState,
  ErrorState,
  InlineLoading,
  Input,
  Meter,
  PageHeader,
  Pill,
  RowActions,
  Select,
  SlideOver,
  Toggle,
} from "@/components/primitives";
import { useApi } from "@/lib/useApi";
import { apiReason, fetchJSON, localConfirmHeaders } from "@/lib/api";
import { navIcon } from "@/lib/nav";
import { fmtDateTime } from "../../../shared/lib/format.ts";
import {
  CHOICE_LABELS,
  EXPERIMENTAL_WARNING,
  GROUP_LABELS,
  PARAM_LABELS,
  SCOPE_NOTES,
  SECRET_LABELS,
  SOURCE_HOWTO,
  SOURCE_LABELS,
  balanceLabel,
  canSave,
  cardPill,
  cardSourceText,
  cardTitle,
  errorText,
  fmtAmount,
  healthText,
  identityText,
  methodOptions,
  paceText,
  providerLabel,
  resetText,
  spendLabel,
  statusVocab,
  windowFrac,
  windowLabel,
  windowTone,
  windowValue,
  type CatalogProvider,
  type CatalogSource,
  type PlanAccount,
  type PlanCard,
  type PlanRunState,
  type PlansResponse,
} from "@/lib/plans";

// Plans & limits: how much of each coding plan and API balance is left.
// The server (internal/planlimitssvc) owns every rule; this page renders
// its page model and collects account edits. Secrets are typed here and
// sent once; no reply ever carries one back.

async function post<T>(path: string, body: unknown): Promise<T> {
  return fetchJSON<T>(path, undefined, {
    method: "POST",
    headers: await localConfirmHeaders(),
    body: JSON.stringify(body),
  });
}

function reason(e: unknown): string {
  return errorText(apiReason(e));
}

export function PlansPage() {
  const res = useApi<PlansResponse>("/api/plans");
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<PlanAccount | null>(null);
  const [history, setHistory] = useState<PlanAccount | null>(null);
  const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null);

  // The page keeps itself current while open (the server polls faster then).
  const reload = res.reload;
  useEffect(() => {
    const t = window.setInterval(() => reload(), 60_000);
    return () => window.clearInterval(t);
  }, [reload]);

  const data = res.data;
  const catalog = data?.catalog ?? [];
  const accounts = useMemo(() => new Map((data?.view.accounts ?? []).map((a) => [a.id, a])), [data]);
  const optIns = useMemo(() => new Set(data?.view.opt_ins ?? []), [data]);
  const cards = data?.view.cards ?? [];

  const tracked = new Set(cards.map((c) => c.provider));
  const untracked = catalog.filter((p) => p.id !== "other" && !tracked.has(p.id)).map((p) => providerLabel(p.id));

  async function act(fn: () => Promise<unknown>, ok?: string) {
    setNotice(null);
    try {
      await fn();
      if (ok) setNotice({ ok: true, text: ok });
    } catch (e) {
      setNotice({ ok: false, text: reason(e) });
    }
    reload();
  }

  return (
    <div className="space-y-6 p-4 sm:p-6">
      <PageHeader
        title="Plans & limits"
        icon={navIcon("plans")}
        sub="How much of each coding plan and API balance you have left."
        right={
          <Button variant="primary" iconLeft={Plus} onClick={() => setAdding(true)} disabled={!data}>
            Add account
          </Button>
        }
      />

      {notice && (
        <Callout tone={notice.ok ? "success" : "warn"} title={notice.ok ? notice.text : "That did not work"}>
          {notice.ok ? null : notice.text}
        </Callout>
      )}

      {res.error && !data ? (
        <ErrorState error={res.error} onRetry={reload} />
      ) : !data ? (
        <InlineLoading block label="Loading plans" />
      ) : cards.length === 0 ? (
        <EmptyState
          title="No plans yet"
          body="Observer already sees Claude and Codex limits when you use them. Add an account to track a plan or an API balance."
        >
          <Button variant="primary" iconLeft={Plus} onClick={() => setAdding(true)}>
            Add account
          </Button>
        </EmptyState>
      ) : (
        <div className="grid gap-3 lg:grid-cols-2">
          {cards.map((c) => (
            <PlanCardView
              key={c.id}
              card={c}
              account={c.account_id ? accounts.get(c.account_id) : undefined}
              onRefresh={(id) => act(() => post(`/api/plans/accounts/${encodeURIComponent(id)}/refresh`, {}), "Refreshed.")}
              onEdit={setEditing}
              onHistory={setHistory}
              onRemove={(id) => act(() => post(`/api/plans/accounts/${encodeURIComponent(id)}/remove`, {}), "Account removed.")}
            />
          ))}
        </div>
      )}

      {data && untracked.length > 0 && (
        <p className="text-[12px] text-fg-3">
          Not tracked yet: {untracked.join(", ")}.{" "}
          <button type="button" className="text-accent hover:underline" onClick={() => setAdding(true)}>
            Add one
          </button>
        </p>
      )}

      {data && (
        <ExperimentalCard
          catalog={catalog}
          optIns={optIns}
          onToggle={(provider, on) => act(() => post("/api/plans/optin", { provider, on }))}
        />
      )}

      {data && (
        <AddAccount
          open={adding}
          catalog={catalog}
          optIns={optIns}
          onClose={() => setAdding(false)}
          onOptIn={(provider) => act(() => post("/api/plans/optin", { provider, on: true }))}
          onSaved={() => {
            setAdding(false);
            setNotice({ ok: true, text: "Account added." });
            reload();
          }}
        />
      )}

      {editing && (
        <EditAccount
          account={editing}
          catalog={catalog}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            setNotice({ ok: true, text: "Account saved." });
            reload();
          }}
        />
      )}

      {history && <HistoryPanel account={history} onClose={() => setHistory(null)} />}
    </div>
  );
}

// ---------- one card ----------

function PlanCardView({
  card,
  account,
  onRefresh,
  onEdit,
  onHistory,
  onRemove,
}: {
  card: PlanCard;
  account?: PlanAccount;
  onRefresh: (id: string) => void;
  onEdit: (a: PlanAccount) => void;
  onHistory: (a: PlanAccount) => void;
  onRemove: (id: string) => void;
}) {
  const pill = cardPill(card);
  const identity = identityText(card);
  const scopeNote = card.scope_reason ? SCOPE_NOTES[card.scope_reason] ?? "" : "";
  const sub = [cardSourceText(card), healthText(card)].filter(Boolean).join(". ");
  const stopped = card.state?.stopped;

  return (
    <Card
      title={cardTitle(card)}
      sub={sub}
      actions={
        <div className="flex items-center gap-1.5">
          {card.experimental && <Pill variant="info">Experimental</Pill>}
          {pill && <Pill variant={pill.tone}>{pill.label}</Pill>}
          {account && (
            <RowActions
              menuLabel={`Actions for ${cardTitle(card)}`}
              actions={[
                { id: "refresh", label: "Refresh now", icon: RefreshCw, onSelect: () => onRefresh(account.id) },
                { id: "edit", label: "Edit", icon: Pencil, onSelect: () => onEdit(account) },
                { id: "history", label: "Show history", icon: History, onSelect: () => onHistory(account) },
                {
                  id: "remove",
                  label: "Remove",
                  icon: Trash2,
                  destructive: true,
                  confirm: { label: "Remove this account", note: "Its saved keys are deleted too." },
                  onSelect: () => onRemove(account.id),
                },
              ]}
            />
          )}
        </div>
      }
    >
      <div className="space-y-2.5">
        {identity && <p className="text-[11px] text-fg-3">{identity}</p>}
        {card.windows.map((w) => {
          const frac = windowFrac(w);
          const reset = resetText(w);
          const pace = paceText(w);
          return (
            <div key={w.key} className="space-y-1">
              <div className="flex items-baseline justify-between gap-3 text-[12px]">
                <span className="text-fg-2">{windowLabel(w.key)}</span>
                <span className="tabular-nums text-fg-1">{windowValue(w)}</span>
              </div>
              {frac != null ? (
                <Meter ratio={Math.min(frac, 1)} tone={windowTone(frac)} label={`${windowLabel(w.key)} used`} />
              ) : null}
              {(reset || pace) && (
                <div className="flex flex-wrap justify-between gap-2 text-[11px] text-fg-3">
                  <span>{reset}</span>
                  <span>{pace}</span>
                </div>
              )}
            </div>
          );
        })}
        {card.balances.map((b, i) => (
          <div key={`b${i}`} className="flex items-baseline justify-between gap-3 text-[12px]">
            <span className="text-fg-2">{balanceLabel(b.kind)}</span>
            <span className="tabular-nums text-fg-1">{fmtAmount(b.unit, b.amount)}</span>
          </div>
        ))}
        {card.spend.map((s, i) => (
          <div key={`s${i}`} className="flex items-baseline justify-between gap-3 text-[12px]">
            <span className="text-fg-2">{spendLabel(s)}</span>
            <span className="tabular-nums text-fg-1">{fmtAmount(s.unit, s.amount)}</span>
          </div>
        ))}
        {card.windows.length === 0 && card.balances.length === 0 && card.spend.length === 0 && (
          <p className="text-[12px] text-fg-3">{emptyCardText(card)}</p>
        )}
        {scopeNote && <p className="text-[11px] text-fg-3">{scopeNote}</p>}
        {stopped && account && (
          <Callout
            tone="warn"
            title="Checks are paused"
            action={
              <Button variant="secondary" size="sm" onClick={() => onEdit(account)}>
                Edit to resume
              </Button>
            }
          >
            Replace the key or sign in to the vendor's tool again, then save the account.
          </Callout>
        )}
      </div>
    </Card>
  );
}

function emptyCardText(c: PlanCard): string {
  if (c.health === "waiting") return "No reading yet.";
  if (c.status === "plan_only" || c.status === "unmetered") return "This plan reports no usage limit.";
  return "Not reported by this plan.";
}

// ---------- experimental switches ----------

function ExperimentalCard({
  catalog,
  optIns,
  onToggle,
}: {
  catalog: CatalogProvider[];
  optIns: Set<string>;
  onToggle: (provider: string, on: boolean) => void;
}) {
  const rows = catalog.filter((p) => p.has_experimental);
  if (rows.length === 0) return null;
  return (
    <Card title="Experimental sources" sub="Off by default. Turn one on only for a provider you want to track this way.">
      <Callout tone="info" title="What experimental means" className="mb-2">
        {EXPERIMENTAL_WARNING}
      </Callout>
      <div className="divide-y divide-line-1">
        {rows.map((p) => (
          <div key={p.id} className="flex items-center justify-between gap-3 py-2">
            <div className="min-w-0">
              <div className="text-[12px] text-fg-1">{providerLabel(p.id)}</div>
              <div className="text-[11px] text-fg-3">
                {p.sources
                  .filter((s) => s.experimental)
                  .map((s) => SOURCE_HOWTO[s.id] ?? "")
                  .join(" ")}
              </div>
            </div>
            <Toggle
              on={optIns.has(p.id)}
              onChange={(next) => onToggle(p.id, next)}
              ariaLabel={`Experimental sources for ${providerLabel(p.id)}`}
            />
          </div>
        ))}
      </div>
    </Card>
  );
}

// ---------- add an account ----------

function Fields({
  src,
  params,
  secrets,
  setParam,
  setSecret,
  keepSecrets,
}: {
  src: CatalogSource;
  params: Record<string, string>;
  secrets: Record<string, string>;
  setParam: (k: string, v: string) => void;
  setSecret: (k: string, v: string) => void;
  keepSecrets?: string[];
}) {
  return (
    <>
      {src.secrets.map((s) => {
        const stored = keepSecrets?.includes(s.key);
        return (
          <Input
            key={`s-${s.key}`}
            type="password"
            autoComplete="off"
            label={SECRET_LABELS[s.key] ?? "Key"}
            placeholder={stored ? "Saved. Type to replace it." : s.required ? "Required" : "Optional"}
            value={secrets[s.key] ?? ""}
            onChange={(e) => setSecret(s.key, e.target.value)}
          />
        );
      })}
      {src.params.map((p) =>
        p.choices && p.choices.length > 0 ? (
          <Select
            key={`p-${p.key}`}
            label={PARAM_LABELS[p.key] ?? "Setting"}
            value={params[p.key] ?? ""}
            onChange={(e) => setParam(p.key, e.target.value)}
            options={[
              ...(p.required ? [] : [{ value: "", label: "Default" }]),
              ...p.choices.map((c) => ({ value: c, label: CHOICE_LABELS[c] ?? c })),
            ]}
          />
        ) : (
          <Input
            key={`p-${p.key}`}
            label={PARAM_LABELS[p.key] ?? "Setting"}
            inputMode={p.numeric ? "decimal" : undefined}
            placeholder={p.required ? "Required" : "Optional"}
            value={params[p.key] ?? ""}
            onChange={(e) => setParam(p.key, e.target.value)}
          />
        ),
      )}
    </>
  );
}

type ProbeReply = { ok: boolean; outcome?: string };

function AddAccount({
  open,
  catalog,
  optIns,
  onClose,
  onOptIn,
  onSaved,
}: {
  open: boolean;
  catalog: CatalogProvider[];
  optIns: Set<string>;
  onClose: () => void;
  onOptIn: (provider: string) => void;
  onSaved: () => void;
}) {
  const [provider, setProvider] = useState("");
  const [source, setSource] = useState("");
  const [label, setLabel] = useState("");
  const [params, setParams] = useState<Record<string, string>>({});
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setProvider("");
      setSource("");
      setLabel("");
      setParams({});
      setSecrets({});
      setMsg(null);
    }
  }, [open]);

  const prov = catalog.find((p) => p.id === provider);
  const methods = methodOptions(prov);
  const src = methods.find((s) => s.id === source);
  const needsSwitch = !!src?.experimental && !optIns.has(provider);

  function pickProvider(id: string) {
    setProvider(id);
    const first = methodOptions(catalog.find((p) => p.id === id))[0];
    setSource(first?.id ?? "");
    setParams({});
    setSecrets({});
    setMsg(null);
  }

  const body = () => ({ provider, source, label, params, secrets });

  async function test() {
    setBusy(true);
    setMsg(null);
    try {
      const r = await post<ProbeReply>("/api/plans/probe", body());
      setMsg(r.ok ? "It works. Save to start tracking." : `Check failed: ${statusVocab(r.outcome ?? "error").label}.`);
    } catch (e) {
      setMsg(reason(e));
    } finally {
      setBusy(false);
    }
  }

  async function save() {
    setBusy(true);
    setMsg(null);
    try {
      await post("/api/plans/accounts", body());
      onSaved();
    } catch (e) {
      setMsg(reason(e));
    } finally {
      setBusy(false);
    }
  }

  const groups = ["plans", "balance"];
  const ready = !!src && !needsSwitch && canSave(src, params, secrets);
  const notReadyReason = !src
    ? "Choose a provider and how to read it."
    : needsSwitch
      ? "Turn on experimental sources for this provider first."
      : "Fill in every required field.";

  return (
    <SlideOver
      open={open}
      onClose={onClose}
      title="Add account"
      subtitle="Keys stay in your system keychain and are sent only to that vendor."
      footer={
        <div className="flex items-center justify-end gap-2">
          {src && !src.passive && src.id !== "manual" && (
            <Button variant="secondary" onClick={test} disabled={!ready || busy} title={ready ? undefined : notReadyReason}>
              Test
            </Button>
          )}
          <Button
            variant="primary"
            onClick={save}
            disabled={!ready || busy}
            loading={busy}
            title={ready ? undefined : notReadyReason}
          >
            Save
          </Button>
        </div>
      }
    >
      <div className="space-y-3">
        <Select label="Provider" value={provider} onChange={(e) => pickProvider(e.target.value)}>
          <option value="">Choose a provider</option>
          {groups.map((g) => (
            <optgroup key={g} label={GROUP_LABELS[g]}>
              {catalog
                .filter((p) => p.group === g)
                .map((p) => (
                  <option key={p.id} value={p.id}>
                    {providerLabel(p.id)}
                  </option>
                ))}
            </optgroup>
          ))}
        </Select>

        {prov && (
          <Select
            label="How to read it"
            value={source}
            onChange={(e) => {
              setSource(e.target.value);
              setParams({});
              setSecrets({});
              setMsg(null);
            }}
            options={methods.map((s) => ({
              value: s.id,
              label: `${SOURCE_LABELS[s.id] ?? "Other"}${s.experimental ? " (experimental)" : ""}`,
            }))}
          />
        )}

        {src && <p className="text-[12px] text-fg-3">{SOURCE_HOWTO[src.id] ?? ""}</p>}

        {src && needsSwitch && (
          <Callout
            tone="warn"
            title="Turn on experimental sources first"
            action={
              <Button variant="secondary" size="sm" onClick={() => onOptIn(provider)}>
                Turn on for {providerLabel(provider)}
              </Button>
            }
          >
            {EXPERIMENTAL_WARNING}
          </Callout>
        )}

        {src && !needsSwitch && (
          <>
            <Input label="Name" placeholder="Optional, for example Work" value={label} onChange={(e) => setLabel(e.target.value)} />
            <Fields
              src={src}
              params={params}
              secrets={secrets}
              setParam={(k, v) => setParams((p) => ({ ...p, [k]: v }))}
              setSecret={(k, v) => setSecrets((s) => ({ ...s, [k]: v }))}
            />
          </>
        )}

        {msg && <p className="text-[12px] text-fg-2" role="status">{msg}</p>}
      </div>
    </SlideOver>
  );
}

// ---------- edit an account ----------

function EditAccount({
  account,
  catalog,
  onClose,
  onSaved,
}: {
  account: PlanAccount;
  catalog: CatalogProvider[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const src = catalog.flatMap((p) => p.sources).find((s) => s.id === account.source);
  const [label, setLabel] = useState(account.label);
  const [enabled, setEnabled] = useState(account.enabled);
  const [params, setParams] = useState<Record<string, string>>({ ...(account.params ?? {}) });
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);

  async function save() {
    setBusy(true);
    setMsg(null);
    try {
      const typed = Object.fromEntries(Object.entries(secrets).filter(([, v]) => v.trim() !== ""));
      await post(`/api/plans/accounts/${encodeURIComponent(account.id)}`, {
        label,
        enabled,
        params,
        secrets: Object.keys(typed).length > 0 ? typed : undefined,
      });
      onSaved();
    } catch (e) {
      setMsg(reason(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <SlideOver
      open
      onClose={onClose}
      title={`Edit ${providerLabel(account.provider)}`}
      subtitle={SOURCE_LABELS[account.source] ?? ""}
      footer={
        <div className="flex justify-end">
          <Button variant="primary" onClick={save} disabled={busy} loading={busy}>
            Save
          </Button>
        </div>
      }
    >
      <div className="space-y-3">
        <Input label="Name" value={label} onChange={(e) => setLabel(e.target.value)} />
        <Toggle on={enabled} onChange={setEnabled} label="Check this account" />
        {src && (
          <Fields
            src={src}
            params={params}
            secrets={secrets}
            keepSecrets={account.secrets}
            setParam={(k, v) => setParams((p) => ({ ...p, [k]: v }))}
            setSecret={(k, v) => setSecrets((s) => ({ ...s, [k]: v }))}
          />
        )}
        {msg && <p className="text-[12px] text-fg-2" role="status">{msg}</p>}
      </div>
    </SlideOver>
  );
}

// ---------- history ----------

type HistoryResponse = {
  snapshots: { observed_at: string; status: string }[];
  runs: { started_at: string; outcome: string }[];
};

function HistoryPanel({ account, onClose }: { account: PlanAccount; onClose: () => void }) {
  const res = useApi<HistoryResponse>(`/api/plans/accounts/${encodeURIComponent(account.id)}/history`);
  const runs = res.data?.runs ?? [];
  return (
    <SlideOver
      open
      onClose={onClose}
      title={`${providerLabel(account.provider)} history`}
      subtitle={nextCheckText(account.state)}
      footer={
        <div className="flex justify-end">
          <Button variant="secondary" iconLeft={RefreshCw} onClick={res.reload}>
            Reload
          </Button>
        </div>
      }
    >
      {res.error ? (
        <ErrorState error={res.error} onRetry={res.reload} />
      ) : !res.data ? (
        <InlineLoading block label="Loading history" />
      ) : (
        <div className="space-y-3 text-[12px]">
          <p className="text-fg-3">{res.data.snapshots.length} readings in the last 7 days.</p>
          {runs.length === 0 ? (
            <p className="text-fg-3">No checks yet.</p>
          ) : (
            <ul className="divide-y divide-line-1">
              {runs.map((r, i) => {
                const v = statusVocab(r.outcome);
                return (
                  <li key={i} className="flex items-center justify-between gap-3 py-1.5">
                    <span className="text-fg-2">{fmtDateTime(r.started_at)}</span>
                    {v.label ? <Pill variant={v.tone}>{v.label}</Pill> : <span className="text-success">OK</span>}
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      )}
    </SlideOver>
  );
}

function nextCheckText(s: PlanRunState | undefined): string {
  if (!s) return "";
  if (s.stopped) return "Checks are paused.";
  if (!s.next_due || s.next_due.startsWith("0001")) return "";
  return `Next check ${fmtDateTime(s.next_due)}`;
}
