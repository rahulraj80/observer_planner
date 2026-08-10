import { useEffect, useMemo, useState } from "react";
import { Pill } from "@/components/primitives";
import { HelpInd } from "@/components/HelpInd";
import { fetchJSON } from "@/lib/api";
import { markRestartPending } from "@/lib/restartPending";
import type { ConfigResponse, LLMConnectionConfig, LLMConfig } from "@/lib/types";

// LLMConnectionsCard — Settings → LLM analysis. Edits the [llm.connections]
// map: add/rename/remove connections, edit base_url / kind / model list, save
// an API key to the OS keychain, and run a live health probe.
//
// Two seams:
//   - PUT /api/config/section/llm writes the Connections map to config.toml
//     (the generic section save, validated via config.Validate). Keys are
//     NEVER part of that body — config holds only the api_key_keychain LABEL.
//   - PUT /api/config/llm/key/<id> stores the literal key in the OS keychain
//     (0600-file fallback) and sets the label. DELETE clears it.
//   - POST /api/config/llm/test/<id> probes the endpoint (reachability +
//     model list), resolving env then keychain credentials.
//
// Restart-honest like every non-pricing config write: the briefing runner
// binds connections at daemon start.

const KINDS = ["openai_compat", "anthropic", "local_openai_compat"];

type Draft = {
  id: string;
  conn: LLMConnectionConfig;
};

function blankDraft(id: string): Draft {
  return {
    id,
    conn: {
      Name: id,
      Kind: "openai_compat",
      BaseURL: "",
      APIKeyEnv: "",
      APIKeyKeychain: "",
      Models: [],
      DefaultModel: "",
      TaskRoles: [],
      Enabled: true,
      AllowRemote: false,
      TimeoutMS: 0,
      MaxTokens: 0,
      NumCtx: 0,
      Headers: {},
    },
  };
}

function connsOf(config: ConfigResponse | null): Record<string, LLMConnectionConfig> {
  const c = config?.config?.LLM?.Connections;
  return c ?? {};
}

type KeySaveResponse = {
  saved: boolean;
  connection_id: string;
  api_key_keychain: string;
  backend: string;
};

type TestResponse = {
  ok: boolean;
  health: { ok: boolean; last_check: string; latency_ms: number; message: string };
  models: string[];
};

export function LLMConnectionsCard({
  config,
  onReload,
}: {
  config: ConfigResponse | null;
  onReload: () => void;
}) {
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [newId, setNewId] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState("");
  const [testResult, setTestResult] = useState<Record<string, TestResponse>>({});
  const [keyStatus, setKeyStatus] = useState<Record<string, KeySaveResponse>>({});
  const [showKeyFor, setShowKeyFor] = useState<string | null>(null);
  const [keyInput, setKeyInput] = useState<Record<string, string>>({});

  const stored = useMemo(() => connsOf(config), [config]);

  useEffect(() => {
    setDrafts((prev) => {
      const next: Record<string, Draft> = {};
      for (const [id, conn] of Object.entries(stored)) {
        next[id] = prev[id] && prev[id].id === id ? prev[id] : { id, conn: { ...conn } };
      }
      return next;
    });
  }, [stored]);

  const dirty = useMemo(() => {
    const entries = Object.entries(stored);
    if (entries.length !== Object.keys(drafts).length) return true;
    return entries.some(([id, conn]) => JSON.stringify(drafts[id]?.conn) !== JSON.stringify(conn));
  }, [stored, drafts]);

  const updateConn = (id: string, patch: Partial<LLMConnectionConfig>) => {
    setDrafts((prev) => {
      const cur = prev[id];
      if (!cur) return prev;
      return { ...prev, [id]: { ...cur, conn: { ...cur.conn, ...patch } } };
    });
    setSaved(false);
  };

  const addConnection = () => {
    const id = newId.trim().replace(/\s+/g, "-");
    if (!id) return;
    if (drafts[id]) {
      setError("connection id already exists");
      return;
    }
    setDrafts((prev) => ({ ...prev, [id]: blankDraft(id) }));
    setNewId("");
    setError("");
  };

  const removeConnection = (id: string) => {
    setDrafts((prev) => {
      const next = { ...prev };
      delete next[id];
      return next;
    });
    setError("");
    setSaved(false);
  };

  const save = async () => {
    setBusy(true);
    setError("");
    try {
      const connections: Record<string, LLMConnectionConfig> = {};
      for (const d of Object.values(drafts)) {
        const prior = stored[d.id];
        // Headers are a hand-written-config-only shape: preserve the prior
        // value silently (the form never edits them).
        connections[d.id] = { ...d.conn, Headers: prior?.Headers ?? d.conn.Headers ?? {} };
      }
      const sec: LLMConfig = { Connections: connections };
      await fetchJSON("/api/config/section/llm", undefined, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(sec),
      });
      markRestartPending("llm");
      setSaved(true);
      onReload();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const saveKey = async (id: string) => {
    const key = (keyInput[id] ?? "").trim();
    if (!key) return;
    setBusy(true);
    setError("");
    try {
      const res = await fetchJSON<KeySaveResponse>(`/api/config/llm/key/${id}`, undefined, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ api_key: key }),
      });
      setKeyStatus((prev) => ({ ...prev, [id]: res }));
      setKeyInput((prev) => ({ ...prev, [id]: "" }));
      setShowKeyFor(null);
      updateConn(id, { APIKeyKeychain: res.api_key_keychain });
      setSaved(false);
      onReload();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const deleteKey = async (id: string) => {
    setBusy(true);
    setError("");
    try {
      await fetchJSON(`/api/config/llm/key/${id}`, undefined, { method: "DELETE" });
      setKeyStatus((prev) => ({ ...prev, [id]: undefined as never }));
      updateConn(id, { APIKeyKeychain: "" });
      onReload();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const testConnection = async (id: string) => {
    setBusy(true);
    setError("");
    try {
      const res = await fetchJSON<TestResponse>(`/api/config/llm/test/${id}`, undefined, {
        method: "POST",
      });
      setTestResult((prev) => ({ ...prev, [id]: res }));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const ids = Object.keys(drafts).sort();

  return (
    <div className="space-y-3">
      <div className="rounded-3 border border-line-2 bg-bg-2 p-4 text-[11.5px]">
        <div className="text-[12px] font-semibold text-fg-1">
          LLM analysis connections — [llm.connections]
          <HelpInd id="card.settings_llm_connections" />
        </div>
        <p className="m-0 mt-0.5 text-fg-3">
          Optional endpoints the daily briefing can point analysis at (narrative upgrade + grounded Q&amp;A).
          Nothing is configured by default — with no connections the briefing is data-only. A loopback host
          (Ollama / vLLM / llama.cpp) is keyless; a remote provider needs{" "}
          <code className="rounded-1 bg-bg-1 px-1 font-mono text-[10.5px]">allow_remote</code> plus a key, which
          is stored in the OS keychain — <code className="rounded-1 bg-bg-1 px-1 font-mono text-[10.5px]">config.toml</code>{" "}
          holds only the label, never the key. Restart required after save.
        </p>
        {error && <p className="m-0 mt-2 text-danger">{error}</p>}
      </div>

      {ids.map((id) => {
        const d = drafts[id];
        const test = testResult[id];
        const key = keyStatus[id];
        const hasKey = Boolean(d.conn.APIKeyKeychain);
        return (
          <div key={id} className="rounded-3 border border-line-2 bg-bg-2 p-4 text-[11.5px]">
            <div className="flex items-center justify-between gap-2">
              <div className="text-[12px] font-semibold text-fg-1">
                <code className="font-mono">{id}</code>
                {!d.conn.Enabled && <Pill variant="warn">disabled</Pill>}
              </div>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => testConnection(id)}
                  disabled={busy}
                  className="rounded-2 border border-line-1 bg-bg-1 px-3 py-1.5 text-[12px] text-fg-1 hover:border-line-2 disabled:opacity-50"
                >
                  Test
                </button>
                <button
                  type="button"
                  onClick={() => removeConnection(id)}
                  disabled={busy}
                  className="rounded-2 border border-line-1 bg-bg-1 px-3 py-1.5 text-[12px] text-danger hover:border-line-2 disabled:opacity-50"
                >
                  Remove
                </button>
              </div>
            </div>

            <div className="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2">
              <Field label="Name">
                <input
                  className="w-full rounded-2 border border-line-1 bg-bg-1 px-2 py-1.5 text-[11.5px] text-fg-1 focus:border-accent focus:outline-none"
                  value={d.conn.Name}
                  onChange={(e) => updateConn(id, { Name: e.target.value })}
                />
              </Field>
              <Field label="Kind">
                <select
                  className="w-full rounded-2 border border-line-1 bg-bg-1 px-2 py-1.5 text-[11.5px] text-fg-1 focus:border-accent focus:outline-none"
                  value={d.conn.Kind}
                  onChange={(e) => updateConn(id, { Kind: e.target.value })}
                >
                  {KINDS.map((k) => (
                    <option key={k} value={k}>
                      {k}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Base URL" wide>
                <input
                  className="w-full rounded-2 border border-line-1 bg-bg-1 px-2 py-1.5 text-[11.5px] text-fg-1 focus:border-accent focus:outline-none"
                  value={d.conn.BaseURL}
                  placeholder="http://127.0.0.1:11434/v1"
                  onChange={(e) => updateConn(id, { BaseURL: e.target.value })}
                />
              </Field>
              <Field label="Default model">
                <input
                  className="w-full rounded-2 border border-line-1 bg-bg-1 px-2 py-1.5 text-[11.5px] text-fg-1 focus:border-accent focus:outline-none"
                  value={d.conn.DefaultModel}
                  placeholder="openai/gpt-oss-20b:free"
                  onChange={(e) => updateConn(id, { DefaultModel: e.target.value })}
                />
              </Field>
              <Field label="Models (comma-separated)">
                <input
                  className="w-full rounded-2 border border-line-1 bg-bg-1 px-2 py-1.5 text-[11.5px] text-fg-1 focus:border-accent focus:outline-none"
                  value={(d.conn.Models ?? []).join(", ")}
                  onChange={(e) =>
                    updateConn(id, {
                      Models: e.target.value
                        .split(",")
                        .map((s) => s.trim())
                        .filter(Boolean),
                    })
                  }
                />
              </Field>
              <Field label="Task roles (comma-separated: narrative, ask, summarize, triage)">
                <input
                  className="w-full rounded-2 border border-line-1 bg-bg-1 px-2 py-1.5 text-[11.5px] text-fg-1 focus:border-accent focus:outline-none"
                  value={(d.conn.TaskRoles ?? []).join(", ")}
                  onChange={(e) =>
                    updateConn(id, {
                      TaskRoles: e.target.value
                        .split(",")
                        .map((s) => s.trim())
                        .filter(Boolean),
                    })
                  }
                />
              </Field>
              <Field label="API key env var (a NAME, never the key)">
                <input
                  className="w-full rounded-2 border border-line-1 bg-bg-1 px-2 py-1.5 font-mono text-[11.5px] text-fg-1 focus:border-accent focus:outline-none"
                  value={d.conn.APIKeyEnv}
                  placeholder="FREELMAPI_KEY"
                  onChange={(e) => updateConn(id, { APIKeyEnv: e.target.value })}
                />
              </Field>
              <Field label="Knobs (timeout_ms / max_tokens / num_ctx)">
                <div className="grid grid-cols-3 gap-2">
                  {(["TimeoutMS", "MaxTokens", "NumCtx"] as const).map((k) => (
                    <input
                      key={k}
                      type="number"
                      className="w-full rounded-2 border border-line-1 bg-bg-1 px-2 py-1.5 text-[11.5px] text-fg-1 focus:border-accent focus:outline-none"
                      value={d.conn[k] ?? 0}
                      onChange={(e) => updateConn(id, { [k]: Number(e.target.value) })}
                    />
                  ))}
                </div>
              </Field>
              <div className="flex flex-col gap-1.5">
                <label className="flex items-center gap-2 text-fg-2">
                  <input
                    type="checkbox"
                    className="h-3.5 w-3.5"
                    checked={d.conn.Enabled}
                    onChange={(e) => updateConn(id, { Enabled: e.target.checked })}
                  />
                  Enabled
                </label>
                <label className="flex items-center gap-2 text-fg-2">
                  <input
                    type="checkbox"
                    className="h-3.5 w-3.5"
                    checked={d.conn.AllowRemote}
                    onChange={(e) => updateConn(id, { AllowRemote: e.target.checked })}
                  />
                  Allow remote (opt-in egress)
                </label>
              </div>
            </div>

            {test && (
              <div className="mt-3 flex flex-wrap items-center gap-2 text-[11.5px]">
                {test.ok ? (
                  <Pill variant="success">
                    reachable — {test.models.length} model(s) in {test.health.latency_ms}ms
                  </Pill>
                ) : (
                  <Pill variant="danger">failed: {test.health.message}</Pill>
                )}
                {test.models.length > 0 && (
                  <span className="text-fg-3">
                    {test.models.slice(0, 6).join(", ")}
                    {test.models.length > 6 && ` +${test.models.length - 6} more`}
                  </span>
                )}
              </div>
            )}

            <div className="mt-3 flex flex-wrap items-center gap-2">
              {showKeyFor === id ? (
                <>
                  <input
                    type="password"
                    className="w-56 rounded-2 border border-line-1 bg-bg-1 px-2 py-1.5 text-[11.5px] text-fg-1 focus:border-accent focus:outline-none"
                    value={keyInput[id] ?? ""}
                    placeholder="paste the key…"
                    onChange={(e) => setKeyInput((prev) => ({ ...prev, [id]: e.target.value }))}
                  />
                  <button
                    type="button"
                    onClick={() => saveKey(id)}
                    disabled={busy || !(keyInput[id] ?? "").trim()}
                    className="rounded-2 bg-accent px-3 py-1.5 text-[12px] font-semibold text-accent-on transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
                  >
                    Save key
                  </button>
                  <button
                    type="button"
                    onClick={() => setShowKeyFor(null)}
                    className="rounded-2 border border-line-1 bg-bg-1 px-3 py-1.5 text-[12px] text-fg-1 hover:border-line-2"
                  >
                    Cancel
                  </button>
                </>
              ) : (
                <button
                  type="button"
                  onClick={() => setShowKeyFor(id)}
                  disabled={busy}
                  className="rounded-2 border border-line-1 bg-bg-1 px-3 py-1.5 text-[12px] text-fg-1 hover:border-line-2 disabled:opacity-50"
                >
                  {hasKey ? "Replace key" : "Save API key"}
                </button>
              )}
              {hasKey && (
                <span className="text-fg-3">
                  key stored in {key?.backend ?? "OS keychain"} as{" "}
                  <code className="rounded-1 bg-bg-1 px-1 font-mono text-[10.5px]">
                    {d.conn.APIKeyKeychain}
                  </code>{" "}
                  <button
                    type="button"
                    onClick={() => deleteKey(id)}
                    className="text-danger underline hover:no-underline"
                  >
                    remove
                  </button>
                </span>
              )}
            </div>
          </div>
        );
      })}

      <div className="rounded-3 border border-dashed border-line-2 bg-bg-2 p-4 text-[11.5px]">
        <div className="text-[12px] font-semibold text-fg-1">Add a connection</div>
        <div className="mt-2 flex items-center gap-2">
          <input
            className="w-56 rounded-2 border border-line-1 bg-bg-1 px-2 py-1.5 text-[11.5px] text-fg-1 focus:border-accent focus:outline-none"
            value={newId}
            placeholder="id, e.g. local-ollama"
            onChange={(e) => setNewId(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") addConnection();
            }}
          />
          <button
            type="button"
            onClick={addConnection}
            disabled={!newId.trim()}
            className="rounded-2 border border-line-1 bg-bg-1 px-3 py-1.5 text-[12px] text-fg-1 hover:border-line-2 disabled:opacity-50"
          >
            Add
          </button>
        </div>
      </div>

      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={save}
          disabled={busy || !dirty}
          className="rounded-2 bg-accent px-3 py-1.5 text-[12px] font-semibold text-accent-on transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
        >
          Save connections
        </button>
        {saved && <Pill variant="warn">saved — restart the daemon to apply</Pill>}
        {!dirty && !saved && <span className="text-fg-3">No unsaved changes</span>}
      </div>
    </div>
  );
}

function Field({
  label,
  wide,
  children,
}: {
  label: string;
  wide?: boolean;
  children: React.ReactNode;
}) {
  return (
    <label className={wide ? "md:col-span-2" : ""}>
      <div className="mb-1 text-fg-3">{label}</div>
      {children}
    </label>
  );
}
