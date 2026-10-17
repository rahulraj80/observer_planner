import { useMemo } from "react";
import { GUARD_RULE_CATALOG } from "@shared/lib/guardCatalog";
import { useApi } from "@/lib/useApi";
import type {
  ActionsResponse,
  CostSummary,
  PricingDefaultsResponse,
  ProjectsResponse,
  SessionsResponse,
  ToolsStatusResponse,
} from "@/lib/types";
import {
  endUserOptions,
  guardRuleOptions,
  mcpServerOptions,
  modelOptions,
  npmPackagesFromCommands,
  pricedModelIds,
  projectFileOptions,
  projectRootOptions,
  sessionOptions,
  toolOptions,
  type PickOption,
} from "@/lib/pickerOptions";

// usePickerSources: the React side of lib/pickerOptions (D-571, lane
// PICKERS-2). One hook per known set the node dashboard offers in a picker,
// each a single read of the endpoint that already owns the set, shared
// through the query cache so several pickers on one page ask the daemon once.
// Pass `enabled = false` to skip the fetch until the picker is on screen.

export type PickerSource = { options: PickOption[]; loading: boolean };

/** Models the cost engine prices on this node (built-in table + overrides). */
export function usePricedModels(enabled = true): PickerSource & { ids: string[] } {
  const api = useApi<PricingDefaultsResponse>(enabled ? "/api/config/pricing/defaults" : null);
  const ids = useMemo(() => pricedModelIds(api.data?.defaults), [api.data]);
  const options = useMemo(() => ids.map((id) => ({ id, label: id })), [ids]);
  return { ids, options, loading: api.loading };
}

/** Models used on this node (last 90 days) first, then every priced model. */
export function useModelOptions(enabled = true): PickerSource {
  const used = useApi<CostSummary>(enabled ? "/api/models" : null, { days: 90 });
  const priced = usePricedModels(enabled);
  const options = useMemo(() => modelOptions(used.data?.rows ?? [], priced.ids), [used.data, priced.ids]);
  return { options, loading: used.loading || priced.loading };
}

/** Projects this node has seen, as project roots. */
export function useProjectRootOptions(enabled = true): PickerSource {
  const api = useApi<ProjectsResponse>(enabled ? "/api/projects" : null);
  const options = useMemo(() => projectRootOptions(api.data?.rows ?? []), [api.data]);
  return { options, loading: api.loading };
}

/** Recent sessions (the newest 200), newest first. */
export function useSessionOptions(enabled = true): PickerSource {
  const api = useApi<SessionsResponse>(enabled ? "/api/sessions" : null, { limit: 200 });
  const options = useMemo(() => sessionOptions(api.data?.rows ?? []), [api.data]);
  return { options, loading: api.loading };
}

/** AI tools this binary knows, detected ones first. */
export function useToolOptions(enabled = true): PickerSource {
  const api = useApi<ToolsStatusResponse>(enabled ? "/api/tools/status" : null);
  const options = useMemo(() => toolOptions(api.data?.tools ?? []), [api.data]);
  return { options, loading: api.loading };
}

/** MCP servers configured in this machine's AI tools. */
export function useMCPServerOptions(enabled = true): PickerSource {
  const api = useApi<{ servers: { name: string; client?: string }[] | null }>(enabled ? "/api/guard/mcp" : null);
  const options = useMemo(() => mcpServerOptions(api.data?.servers ?? []), [api.data]);
  return { options, loading: api.loading };
}

/** End-user ids seen on recent admission verdicts plus the ones a draft
 *  policy already names. */
export function useEndUserOptions(named: readonly string[], enabled = true): PickerSource {
  const api = useApi<{ user?: string }[]>(enabled ? "/api/obs/admission/verdicts" : null, { limit: 500 });
  const key = named.join("\n");
  const options = useMemo(
    () => endUserOptions(Array.isArray(api.data) ? api.data : [], key ? key.split("\n") : []),
    [api.data, key],
  );
  return { options, loading: api.loading };
}

/** Criterion ids of the admission policy in force. */
export function useCriterionIds(enabled = true): { ids: string[]; loading: boolean } {
  const api = useApi<{ policy?: { criteria?: { id: string }[] } }>(enabled ? "/api/obs/admission/policy" : null);
  const ids = useMemo(() => (api.data?.policy?.criteria ?? []).map((c) => c.id).filter(Boolean), [api.data]);
  return { ids, loading: api.loading };
}

/** npm packages the MCP servers configured on this machine run. */
export function useMCPPackageOptions(enabled = true): PickerSource {
  const api = useApi<{ servers: { name: string; command?: string }[] | null }>(enabled ? "/api/guard/mcp" : null);
  const options = useMemo(
    () => npmPackagesFromCommands((api.data?.servers ?? []).map((s) => s.command)).map((p) => ({ id: p, label: p })),
    [api.data],
  );
  return { options, loading: api.loading };
}

/** Files one project's sessions read or edited (last 90 days, newest 500
 *  actions), as project-relative paths. */
export function useProjectFileOptions(root: string): PickerSource {
  const api = useApi<ActionsResponse>(root ? "/api/actions" : null, { project: root, limit: 500, days: 90 }, [root]);
  const options = useMemo(() => projectFileOptions(api.data?.rows ?? [], root), [api.data, root]);
  return { options, loading: api.loading };
}

/** The guard rule catalog, grouped by category (static per build). */
export const GUARD_RULE_OPTIONS: PickOption[] = guardRuleOptions(GUARD_RULE_CATALOG);
