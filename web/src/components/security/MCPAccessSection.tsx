import type { ColumnDef } from "@tanstack/react-table";
import { ChartShell, ErrorState, InlineLoading, Pill, Table, Tooltip } from "@/components/primitives";
import { DataTable } from "@/components/DataTable";
import { StatusTile } from "./StatusTile";
import { useApi } from "@/lib/useApi";
import { markRestartPending } from "@/lib/restartPending";
import { CommandJobButton } from "@/components/CommandJobButton";
import {
  approvedServerCount,
  approvedServerRows,
  cellLabel,
  cellNote,
  cellTier,
  chainLine,
  coverageGrid,
  coverageSummaryLine,
  coverageTally,
  coverageTierVariant,
  coverageTransports,
  effectiveLine,
  mcpAccessSectionState,
  relayLine,
  remoteLine,
  safeConnectURL,
  type ApprovedServerRow,
  type MCPAccessResponse,
  type MCPAccessView,
} from "@/lib/mcpAccess";

// MCPAccessSection - the node Security page's "MCP access" section (Agent
// Access P10, doc3 §15 "node dashboard"): relay status, the approved
// servers the org grant carries, the client x transport coverage matrix
// (§12.7 coverage honesty) and the node-mcp-relay effective state (§12.8).
// Everything is read from GET /api/mcp-access/status, the same derivation
// `observer mcp status` prints; the wording lives in @/lib/mcpAccess (pure,
// tested). The Actions row runs the `observer mcp` verbs (wrap, unwrap, relay
// on/off, doctor) as dashboard jobs; Connect opens the org dashboard.

export function MCPAccessSection() {
  const api = useApi<MCPAccessResponse>("/api/mcp-access/status", { verify: 1 });
  const state = mcpAccessSectionState(api.data, api.loading, api.error ? api.error.message : null);

  return (
    <ChartShell
      title="MCP access"
      sub={
        <>
          The node MCP relay mediates your AI clients' MCP servers against your organization's
          tools.mcp_access grant. This shows what it actually covers on this machine: the relay
          mediates; the hook and proxy points are best-effort; anything else is ungoverned. Same data
          as <code className="font-mono">observer mcp status</code>.
        </>
      }
      right={
        state.kind !== "loading" ? (
          <button
            type="button"
            className="rounded-2 border border-line-2 bg-bg-3 px-2 py-0.5 text-caption text-fg-2 hover:border-accent hover:text-accent"
            onClick={() => api.reload()}
          >
            Refresh
          </button>
        ) : undefined
      }
    >

      {state.kind === "loading" && <InlineLoading label="Loading MCP access" block />}
      {state.kind === "error" && (
        <ErrorState
          title="Couldn't load MCP access"
          error={state.message}
          onRetry={() => api.reload()}
          className="py-3"
        />
      )}
      {state.kind === "unavailable" && <div className="py-3 text-[11.5px] text-fg-3">{state.message}</div>}
      {state.kind === "disabled" && (
        <div className="space-y-2">
          <div className="text-[11.5px] text-fg-2">{state.message}</div>
          <Notes notes={state.notes} />
          <RelayActions
            enabled={false}
            wrapped={api.data?.available !== false && (api.data?.status.launch_specs ?? 0) > 0}
            onChanged={() => api.reload()}
          />
        </div>
      )}
      {state.kind === "on" && (
        <>
          <MCPAccessDetail view={state.view} notes={state.notes} />
          <RelayActions enabled wrapped={state.view.status.launch_specs > 0} onChanged={() => api.reload()} />
        </>
      )}
    </ChartShell>
  );
}

// RelayActions: the `observer mcp` / `mcp-relay` verbs as dashboard jobs
// (backlog item 6). Each button runs the CLI command itself, so the journal,
// the verbatim backups and the restore rules are the CLI's own. Every write
// asks for a second click that says which files it touches; the relay's
// listener binds or unbinds only when the daemon restarts, which the restart
// banner then offers.
function RelayActions({
  enabled,
  wrapped,
  onChanged,
}: {
  enabled: boolean;
  wrapped: boolean;
  onChanged: () => void;
}) {
  const relayFlipped = () => {
    markRestartPending("mcp_relay");
    onChanged();
  };
  return (
    <div className="mt-4 border-t border-line-1 pt-3">
      <h3 className="mb-2 text-[12px] font-semibold text-fg-0">Actions</h3>
      <div className="flex flex-wrap items-start gap-3">
        {!enabled && (
          <CommandJobButton
            job="mcp-relay-enable"
            variant="primary"
            confirmLabel="Turn on and wrap?"
            armedNote="Sets [mcp_relay].enabled = true and rewrites every installed AI client's MCP config through the relay. Each original is backed up first."
            runningLabel="Turning on…"
            doneText="Relay on. Restart the daemon to bind it."
            onDone={relayFlipped}
          >
            Turn on the relay
          </CommandJobButton>
        )}
        {enabled && (
          <CommandJobButton
            job="mcp-wrap"
            confirmLabel="Wrap client configs?"
            armedNote="Rewrites every installed AI client's MCP entries through the relay. Each original is backed up first; Restore puts it back."
            runningLabel="Wrapping…"
            doneText="Client configs wrapped."
            onDone={onChanged}
          >
            Wrap client configs
          </CommandJobButton>
        )}
        {(wrapped || enabled) && (
          <CommandJobButton
            job="mcp-unwrap"
            confirmLabel="Restore originals?"
            armedNote="Puts every wrapped client MCP config back from its backup. A file you edited since keeps your edits."
            runningLabel="Restoring…"
            doneText="Original configs restored."
            onDone={onChanged}
          >
            Restore original configs
          </CommandJobButton>
        )}
        {enabled && (
          <CommandJobButton
            job="mcp-relay-disable"
            variant="danger"
            confirmLabel="Turn off and restore?"
            armedNote="Sets [mcp_relay].enabled = false and restores every client MCP config. MCP calls on this machine stop being mediated."
            runningLabel="Turning off…"
            doneText="Relay off. Restart the daemon to unbind it."
            onDone={relayFlipped}
          >
            Turn off the relay
          </CommandJobButton>
        )}
        {enabled && (
          <CommandJobButton job="mcp-doctor" requireConfirm={false} runningLabel="Checking…" doneText="Doctor finished.">
            Run doctor
          </CommandJobButton>
        )}
      </div>
    </div>
  );
}

function Notes({ notes }: { notes: string[] }) {
  if (notes.length === 0) return null;
  return (
    <ul className="space-y-0.5 text-[11.5px] text-warn">
      {notes.map((n) => (
        <li key={n}>{n}</li>
      ))}
    </ul>
  );
}

// serverColumns builds the approved-servers table. Sorting stays off: the
// rows keep the registry's vserver-then-server order (approvedServerRows),
// which is what groups a virtual server's servers together.
function serverColumns(connectURL: string): ColumnDef<ApprovedServerRow, unknown>[] {
  return [
    {
      id: "vserver",
      header: "Virtual server",
      enableSorting: false,
      cell: ({ row }) => (
        <span className="font-mono text-[11px] text-fg-1">{row.original.slug || row.original.vserver}</span>
      ),
    },
    {
      id: "server",
      header: "Server",
      enableSorting: false,
      cell: ({ row }) => (
        <span className="font-mono text-[11px] text-fg-1">
          {row.original.server}
          {row.original.pinned && (
            <Pill variant="neutral" className="ml-1.5" title="An approved tool snapshot is pinned for this server">
              pinned
            </Pill>
          )}
        </span>
      ),
    },
    {
      id: "target",
      header: "Target",
      enableSorting: false,
      cell: ({ row }) => (
        <span className="block max-w-[300px] truncate font-mono text-[10.5px] text-fg-3" title={row.original.target}>
          {row.original.target}
        </span>
      ),
    },
    {
      id: "credential",
      header: "Credential",
      enableSorting: false,
      cell: ({ row }) => (
        <span className="text-fg-2">{row.original.perUser ? "per-user sign-in" : row.original.credential}</span>
      ),
    },
    {
      id: "connect",
      header: "",
      enableSorting: false,
      meta: { align: "right" },
      cell: ({ row }) =>
        row.original.perUser && connectURL ? (
          <Tooltip content="Sign in to this server in your org dashboard (My Connections). The token stays with the org; this machine never sees it.">
            <a
              href={connectURL}
              target="_blank"
              rel="noopener noreferrer"
              className="rounded-2 border border-line-2 px-2 py-0.5 text-caption text-fg-2 hover:border-accent hover:text-accent"
            >
              Connect
            </a>
          </Tooltip>
        ) : null,
    },
  ];
}

function MCPAccessDetail({ view, notes }: { view: MCPAccessView; notes: string[] }) {
  const s = view.status;
  const tally = coverageTally(view.coverage_rows);
  const eff = effectiveLine(view.effective_state);
  const chain = chainLine(s, view.chain_verified);
  const servers = approvedServerRows(view.vservers);
  const connectURL = safeConnectURL(view.connect);
  const grid = coverageGrid(view.coverage_rows);
  const transports = coverageTransports(view.coverage_rows);

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-4">
        <StatusTile
          subVisible
          label="Relay"
          value={s.point_status === "effective" ? "on, effective" : `on, ${s.point_status}`}
          sub={relayLine(s)}
          tone={s.point_status === "effective" ? "ok" : "warn"}
        />
        <StatusTile
          subVisible
          label="Approved servers"
          value={String(approvedServerCount(view.vservers))}
          sub={`${view.vservers.length} virtual server${view.vservers.length === 1 ? "" : "s"} in the accepted grant`}
          tone="neutral"
        />
        <StatusTile
          subVisible
          label="Coverage"
          value={`${tally.governed} / ${view.coverage_rows.length} governed`}
          sub={coverageSummaryLine(view.coverage_rows)}
          tone={tally.ungoverned > 0 ? "warn" : "ok"}
        />
        <StatusTile subVisible label="Effective state" value={view.effective_state.status.replace(/_/g, " ")} sub={eff.text} tone={eff.tone} />
      </div>

      <Notes notes={notes} />

      <dl className="grid grid-cols-1 gap-x-4 gap-y-1 text-[11.5px] sm:grid-cols-[max-content_1fr]">
        <dt className="text-fg-3">Remote forwarding</dt>
        <dd className="text-fg-1">{remoteLine(view)}</dd>
        <dt className="text-fg-3">Decision record</dt>
        <dd className={chain.tone === "danger" ? "text-danger" : "text-fg-1"}>{chain.text}</dd>
        <dt className="text-fg-3">Wrapped clients</dt>
        <dd className="text-fg-1">
          {(s.projected_clients ?? []).length > 0
            ? `${(s.projected_clients ?? []).join(", ")} (${s.launch_specs} journaled entr${s.launch_specs === 1 ? "y" : "ies"})`
            : "none verified on disk - use the 'Wrap client configs' button below"}
        </dd>
        <dt className="text-fg-3">Grant</dt>
        <dd className="text-fg-1">
          {s.table_loaded
            ? `v${s.table_version ?? 0}, ${s.table_mode ?? "?"}, ${s.table_rows ?? 0} rows, policy gen ${s.policy_gen ?? 0}`
            : "none accepted yet"}
        </dd>
        {view.effective_state.effective_hash && (
          <>
            <dt className="text-fg-3">Effective hash</dt>
            <dd className="truncate font-mono text-[10.5px] text-fg-2" title={view.effective_state.effective_hash}>
              {view.effective_state.effective_hash}
            </dd>
          </>
        )}
      </dl>

      <div>
        <h3 className="mb-1 text-[12px] font-semibold text-fg-0">Approved servers</h3>
        {servers.length === 0 ? (
          <div className="text-[11.5px] text-fg-3">
            The accepted grant approves no MCP server on this node.
          </div>
        ) : (
          <DataTable<ApprovedServerRow>
            data={servers}
            columns={serverColumns(connectURL)}
            rowKey={(r) => `${r.vserver}/${r.server}`}
            minWidth={560}
          />
        )}
        {servers.some((r) => r.perUser) && !connectURL && (
          <div className="mt-1 text-[11px] text-fg-3">
            Per-user servers are connected in the org dashboard: {view.connect.unavailable ?? "no org dashboard URL is known"}.
          </div>
        )}
      </div>

      <div>
        <h3 className="mb-1 text-[12px] font-semibold text-fg-0">Coverage by client and transport</h3>
        <p className="mb-2 max-w-3xl text-[11px] leading-snug text-fg-3">
          Each cell is what this node actually measured for tool calls and tool lists. Relay-mediated
          is governed; the PreToolUse hook and the proxy filter are best-effort controls (known
          shapes, traffic through the proxy only); anything else is ungoverned unless MDM/EDR and
          network egress controls back it. A cell shows its weaker method's tier.
        </p>
        {/* A client x transport matrix with data-driven columns: the shared
            Table primitive, not DataTable (no record list, nothing to sort). */}
        <Table
          minWidth={420}
          head={
            <tr>
              <th className="py-1.5 pr-3 font-medium">Client</th>
              {transports.map((t) => (
                <th key={t} className="py-1.5 pr-3 font-medium">
                  {t}
                </th>
              ))}
            </tr>
          }
        >
          {grid.map((line) => (
            <tr key={line.client} className="border-b border-line-1/60 last:border-0">
              <td className="py-1.5 pr-3 text-fg-2">{line.client}</td>
              {line.cells.map((cell) => (
                <td key={cell.transport} className="py-1.5 pr-3">
                  <Pill variant={coverageTierVariant(cellTier(cell))} title={cellNote(cell) || undefined}>
                    {cellLabel(cell)}
                  </Pill>
                </td>
              ))}
            </tr>
          ))}
        </Table>
      </div>

      <div className="text-[11px] text-fg-3">
        CLI parity: <code className="font-mono">observer mcp status</code> /{" "}
        <code className="font-mono">observer mcp doctor</code> /{" "}
        <code className="font-mono">observer mcp connect</code>.
      </div>
    </div>
  );
}
