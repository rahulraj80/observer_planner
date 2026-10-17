import { useMemo } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { configEditRequestLink } from "@/lib/requestEntry";
import { BookOpen, Building2, ChartPie, FolderGit2, History, Layers, Lock, Wrench, type LucideIcon } from "lucide-react";
import {
  Card,
  EmptyState,
  ErrorState,
  InlineLoading,
  PageHeader,
  Pill,
  Select,
  TabStrip,
  Table,
  TruncatedPath,
  type TabDef,
} from "@/components/primitives";
import { useApi } from "@/lib/useApi";
import { useUrlTab } from "@/lib/useUrlTab";
import { usePageTabs } from "@/lib/governance";
import { AGENT_CONFIG_TABS, defaultTab, type AgentConfigTab } from "@/lib/nodePageTabs";
import { navIcon } from "@/lib/nav";
import type { ProjectsResponse } from "@/lib/types";
import type { CoverageResponse, HistoryRow, ToolsResponse } from "@/lib/agentConfig";
import { ToolsTab } from "./agentconfig/ToolsTab";
import { CommonTab } from "./agentconfig/CommonTab";
import { InstructionsTab } from "./agentconfig/InstructionsTab";
import { ProjectsTab } from "./agentconfig/ProjectsTab";
import { OrgTab } from "./agentconfig/OrgTab";

// AI tool settings (backlog items 2 and 7, phases P4 and P5). Reads and
// edits each AI coding tool's OWN settings and instruction files, through
// the server's sanity check (internal/agentconfig PlanSave) on every save.
// The server decides everything (what a file holds, which layer wins,
// what a save may change); this page shows it and collects the edit.

const TAB_ICONS: Record<AgentConfigTab, LucideIcon> = {
  tools: Wrench,
  projects: FolderGit2,
  common: Layers,
  instructions: BookOpen,
  organization: Building2,
  coverage: ChartPie,
  history: History,
};

const STATUS_TONE: Record<string, "success" | "info" | "neutral" | "warn"> = {
  generated: "success",
  authored: "info",
  pending: "warn",
  no_surface: "neutral",
};

export function AgentConfigPage() {
  // The tabs the organization left visible on this machine (T-132).
  const pageTabs = usePageTabs("agent-config", AGENT_CONFIG_TABS);
  const [tab, setTab] = useUrlTab(pageTabs.ids, defaultTab(pageTabs));
  const [params, setParams] = useSearchParams();
  const tools = useApi<ToolsResponse>("/api/agent-config/tools");
  const projects = useApi<ProjectsResponse>("/api/projects");

  const editable = useMemo(
    () =>
      (tools.data?.tools ?? [])
        .filter((t) => t.status !== "no_surface")
        .sort((a, b) => Number(b.installed) - Number(a.installed) || a.tool.localeCompare(b.tool)),
    [tools.data],
  );
  const tool = params.get("tool") || editable.find((t) => t.installed)?.tool || editable[0]?.tool || "";
  const project = params.get("project") || "";
  const access = tools.data?.access ?? null;
  const confirmToken = tools.data?.confirm_token ?? "";

  function setParam(k: string, v: string) {
    const next = new URLSearchParams(params);
    if (v) next.set(k, v);
    else next.delete(k);
    setParams(next, { replace: true });
  }

  const tabs: TabDef<AgentConfigTab>[] = pageTabs.ids.map((id) => ({
    id,
    label: AGENT_CONFIG_TABS.labels[id],
    icon: TAB_ICONS[id],
  }));
  const usesTool = tab === "tools" || tab === "projects" || tab === "instructions";
  const usesProject = tab !== "coverage" && tab !== "history" && tab !== "organization";

  return (
    <div className="space-y-5 p-4 sm:p-6">
      <PageHeader
        icon={navIcon("agent-config")}
        title="AI tool settings"
        helpId="tab.agent-config"
        sub="Each AI coding tool's own settings and instruction files, with the value that wins and the file that decides it. Every save is checked first, backed up and read back."
      />

      {access?.read_only && (
        <div
          role="status"
          className="flex items-start gap-2 rounded-3 border border-line-2 bg-bg-2 p-3 text-[12px] text-fg-2"
        >
          <Lock size={14} className="mt-0.5 shrink-0 text-fg-3" aria-hidden />
          <span>
            {access.reason || "These settings are read-only on this machine."}{" "}
            {/* T-436: ask the organization to open a setting for a while. */}
            <Link to={configEditRequestLink(tool ? `${tool}/*` : undefined)} className="text-accent hover:underline">
              Ask your organization to let you edit a setting
            </Link>
          </span>
        </div>
      )}

      {(usesTool || usesProject) && (
        <div className="flex flex-wrap items-end gap-3">
          {usesTool && (
            <Select
              label="Tool"
              value={tool}
              onChange={(e) => setParam("tool", e.target.value)}
              className="w-full sm:w-56"
              options={editable.map((t) => ({
                value: t.tool,
                label: t.installed ? t.tool : `${t.tool} (not installed)`,
              }))}
            />
          )}
          {usesProject && (
            <Select
              label="Project"
              value={project}
              onChange={(e) => setParam("project", e.target.value)}
              className="w-full sm:w-80"
              options={[
                { value: "", label: "No project (user settings only)" },
                ...(projects.data?.rows ?? []).map((p) => ({ value: p.root_path, label: p.root_path })),
              ]}
            />
          )}
        </div>
      )}

      <TabStrip tabs={tabs} value={tab} onChange={setTab} idPrefix="agent-config" />

      {tools.error && !tools.data ? (
        <ErrorState error={tools.error} onRetry={tools.reload} />
      ) : !tools.data ? (
        <InlineLoading label="Loading AI tools" />
      ) : (
        <>
          {tab === "tools" && <ToolsTab tool={tool} project={project} access={access} confirmToken={confirmToken} />}
          {tab === "projects" && (
            <ProjectsTab tool={tool} project={project} access={access} confirmToken={confirmToken} />
          )}
          {tab === "common" && <CommonTab project={project} access={access} confirmToken={confirmToken} />}
          {tab === "instructions" && (
            <InstructionsTab tool={tool} project={project} access={access} confirmToken={confirmToken} />
          )}
          {tab === "organization" && <OrgTab confirmToken={confirmToken} />}
          {tab === "coverage" && <CoverageTab />}
          {tab === "history" && <HistoryTab />}
        </>
      )}
    </div>
  );
}

function CoverageTab() {
  const cov = useApi<CoverageResponse>("/api/agent-config/coverage");
  const tools = useApi<ToolsResponse>("/api/agent-config/tools");
  if (cov.error && !cov.data) return <ErrorState error={cov.error} onRetry={cov.reload} />;
  if (!cov.data) return <InlineLoading label="Loading coverage" />;
  const installed = new Map((tools.data?.tools ?? []).map((t) => [t.tool, t.installed]));
  return (
    <div className="space-y-4">
      <Card
        title="Catalogue coverage"
        icon={ChartPie}
        sub="How much of each tool's settings Observer knows: rows, how many are verified against the vendor, and how many it can write."
      >
        <Table
          minWidth={560}
          head={
            <tr>
              <th className="px-2 py-1.5 text-left">Tool</th>
              <th className="px-2 py-1.5 text-left">Source</th>
              <th className="px-2 py-1.5 text-right">Settings</th>
              <th className="px-2 py-1.5 text-right">Verified</th>
              <th className="px-2 py-1.5 text-right">Writable</th>
              <th className="px-2 py-1.5 text-right">Safety</th>
            </tr>
          }
        >
          {cov.data.coverage.map((c) => (
            <tr key={c.tool} className="border-t border-line-1">
              <td className="max-w-[260px] px-2 py-1.5 text-fg-1">
                {c.tool}
                {installed.get(c.tool) && <span className="ml-1 text-[10.5px] text-success">installed</span>}
                {c.verified_against && (
                  <div className="truncate text-[10.5px] text-fg-4" title={c.verified_against}>
                    checked against {c.verified_against}
                  </div>
                )}
              </td>
              <td className="px-2 py-1.5">
                <Pill variant={STATUS_TONE[c.status] ?? "neutral"} case="none" title={c.note}>
                  {c.status.replace("_", " ")}
                </Pill>
              </td>
              <td className="px-2 py-1.5 text-right tabular-nums">{c.rows}</td>
              <td className="px-2 py-1.5 text-right tabular-nums">{c.verified}</td>
              <td className="px-2 py-1.5 text-right tabular-nums">{c.writable}</td>
              <td className="px-2 py-1.5 text-right tabular-nums">{c.dangerous}</td>
            </tr>
          ))}
        </Table>
      </Card>
      <Card title="Pinned vendor schemas" sub="Schemas Observer validates against, offline, by the hash it pinned.">
        <ul className="space-y-1 text-[11.5px]">
          {cov.data.schemas.map((s) => (
            <li key={`${s.tool}-${s.shape}`} className="flex min-w-0 flex-wrap gap-x-2">
              <span className="text-fg-1">{s.tool}</span>
              <span className="min-w-0 break-all text-fg-3">{s.url}</span>
              <span className="text-fg-4">fetched {s.fetched_on}</span>
            </li>
          ))}
        </ul>
      </Card>
    </div>
  );
}

function HistoryTab() {
  const hist = useApi<{ rows: HistoryRow[] }>("/api/agent-config/history", { limit: 100 });
  if (hist.error && !hist.data) return <ErrorState error={hist.error} onRetry={hist.reload} />;
  if (!hist.data) return <InlineLoading label="Loading history" />;
  if (hist.data.rows.length === 0) {
    return (
      <EmptyState
        title="No saves yet"
        body="Every save made here or with observer agent-config is listed with the keys it changed (never their values) and its backup."
      />
    );
  }
  return (
    <Card title="Saves" icon={History} sub="Key names only; values are never recorded here.">
      <Table
        minWidth={760}
        head={
          <tr>
            <th className="px-2 py-1.5 text-left">When</th>
            <th className="px-2 py-1.5 text-left">Tool</th>
            <th className="px-2 py-1.5 text-left">File</th>
            <th className="px-2 py-1.5 text-left">Changed</th>
            <th className="px-2 py-1.5 text-left">From</th>
          </tr>
        }
      >
        {hist.data.rows.map((r) => (
          <tr key={r.id} className="border-t border-line-1 align-top">
            <td className="whitespace-nowrap px-2 py-1.5 font-mono text-[11px] text-fg-3">{r.at.replace("T", " ").replace("Z", "")}</td>
            <td className="px-2 py-1.5 text-fg-1">{r.tool}</td>
            <td className="max-w-[300px] px-2 py-1.5">
              <TruncatedPath value={r.path} className="font-mono text-[11px] text-fg-2" />
              {r.backup_path && <div className="text-[10.5px] text-fg-4">backup kept</div>}
            </td>
            <td className="px-2 py-1.5 text-[11px] text-fg-2">
              <span className="break-all font-mono">{(r.changed_keys ?? []).join(", ") || (r.mode === "raw" ? "whole file" : "")}</span>
              {r.common_key && <Pill case="none" className="ml-1">{r.common_key}</Pill>}
              {r.created && <Pill case="none" className="ml-1">created</Pill>}
            </td>
            <td className="px-2 py-1.5 text-fg-3">{r.origin}</td>
          </tr>
        ))}
      </Table>
    </Card>
  );
}
