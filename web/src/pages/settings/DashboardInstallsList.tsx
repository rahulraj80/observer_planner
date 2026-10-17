import { useLaunchDock } from "@/components/LaunchDock";
import { DashboardInstallRecord } from "@/components/DashboardInstallRecord";
import { useApi } from "@/lib/useApi";
import type { ToolInstallsResponse } from "@/lib/types";

// DashboardInstallsList is the Settings > Connected tools block: every tool
// this dashboard installed (latest record per tool, GET /api/terminal/installs),
// each with the same Uninstall / Forget as the New terminal dialog. The route
// is owner-local; a failed or empty read renders nothing (the block is
// additive to the tools table, never a gate on it).
export function DashboardInstallsList() {
  const list = useApi<ToolInstallsResponse>("/api/terminal/installs");
  const dock = useLaunchDock();
  const rows = list.data?.installs ?? [];
  if (list.error || !list.data?.available || rows.length === 0) return null;
  return (
    <div className="mb-3" data-testid="dashboard-installs-list">
      <div className="mb-1.5 text-[11.5px] font-semibold text-fg-2">Installed from this dashboard</div>
      <div className="space-y-2">
        {rows.map((rec) => (
          <div key={rec.id}>
            <div className="mb-0.5 font-mono text-[11px] text-fg-2">{rec.tool}</div>
            <DashboardInstallRecord
              record={rec}
              onUninstallStarted={(r) => {
                dock.launch({ token: r.handle, tool: `${r.tool} uninstall`, sessionId: "", hasProjectRoot: false });
                list.reload();
              }}
              onChanged={list.reload}
              onOpenTerminal={(h) =>
                dock.launch({ token: h, tool: `${rec.tool} install`, sessionId: "", hasProjectRoot: false })
              }
            />
          </div>
        ))}
      </div>
    </div>
  );
}
