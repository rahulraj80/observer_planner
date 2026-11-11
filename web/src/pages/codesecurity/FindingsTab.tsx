import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { Card } from "@/components/primitives";
import { useApi, type ApiState } from "@/lib/useApi";
import { useComponentState } from "@/lib/useComponents";
import type { ExceptionsResponse } from "@/lib/codeSecurityExceptions";
import { findingsDrillDown, updateFindingsSearch, type SecurityStatus } from "@/lib/codeSecurity";
import { parseFindingsFilters, type FindingGroup } from "@/lib/codeSecurityApi";
import {
  findingsCountLine,
  findingsEmptyHint,
  findingsLoadCounts,
  findingsLoadingLabel,
  REACH_PENDING_NOTE,
} from "@/lib/findingsLabels";
import { FindingsFilterBar, type FindingsFiltersChange } from "./FindingsFilterBar";
import { FindingsCard } from "./FindingsCard";
import { FindingGroupsTable } from "./FindingGroupsTable";
import { FindingDrawer } from "./FindingDrawer";
import { useFindingGroups, useFindingsFacets, useFindingsList } from "./useFindingsList";

// FindingsTab is the Code security page's Findings tab (redesign plan
// docs/plans/code-security-page-redesign-plan-2026-11-06.md section 4.4):
// the filter bar, the count line, the list (grouped by issue by default,
// or ungrouped) and the finding drawer.
//
// The filters live in the URL (lib/codeSecurityApi.ts parse/write), so an
// Overview link (findingsHref) lands on a filtered view and a reload or a
// shared link keeps it; a discrete choice adds a history entry, typing in
// the search box replaces it. The page and sort of each list stay in
// component state and return to page 1 on any filter change. The drawer is
// `?finding=<id>`, beside the filters.
//
// Working surface: calm register, no mascot; loading says what it loads.

// The URL param the finding drawer is addressed by.
const FINDING_PARAM = "finding";

export function FindingsTab({ status }: { status: ApiState<SecurityStatus> }) {
  const [params, setParams] = useSearchParams();
  const filters = useMemo(() => parseFindingsFilters(params), [params]);
  const grouped = filters.group !== "none";
  const facets = useFindingsFacets(filters);
  const list = useFindingsList(filters, !grouped);
  const groups = useFindingGroups(filters.group, filters);
  const projects = useMemo(() => status.data?.projects ?? [], [status.data]);

  const onChange = useCallback<FindingsFiltersChange>(
    (change, history = "push") =>
      setParams((prev) => updateFindingsSearch(prev, change), { replace: history === "replace" }),
    [setParams],
  );
  const onDrill = useCallback(
    (g: FindingGroup) => {
      if (filters.group === "none") return;
      const by = filters.group;
      setParams((prev) => findingsDrillDown(prev, by, g));
    },
    [filters.group, setParams],
  );

  const openFinding = params.get(FINDING_PARAM);
  const setFinding = useCallback(
    (id: string | null) =>
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (id) next.set(FINDING_PARAM, id);
          else next.delete(FINDING_PARAM);
          return next;
        },
        { replace: true },
      ),
    [setParams],
  );
  const pickers = useApi<ExceptionsResponse>("/api/security/exceptions");
  // Explain shows only while security.feedback is selected (the opt-in rule;
  // ExplainButton checks the selection itself).
  const securitySelected = useComponentState().data?.security_selected;

  const counts = findingsLoadCounts(filters, facets.data, status.data?.projects);
  const loadingLabel = findingsLoadingLabel(filters, counts);
  const emptyHint = findingsEmptyHint(filters);
  const countLine = findingsCountLine(
    filters.group,
    grouped ? groups.read.data?.total : null,
    grouped ? facets.data?.total : list.read.data?.total,
  );

  return (
    <div className="space-y-4" data-testid="code-security-findings-tab">
      <Card title="Findings" sub={countLine ?? undefined}>
        <FindingsFilterBar
          filters={filters}
          facets={facets}
          projects={projects}
          projectsLoaded={status.data != null}
          onChange={onChange}
        />
        <div className="mt-4 space-y-2">
          {!grouped && list.reachPending && (
            <p className="m-0 text-[11.5px] text-fg-3" role="status" data-testid="code-security-reach-pending">
              {REACH_PENDING_NOTE}
            </p>
          )}
          {filters.group === "none" ? (
            <FindingsCard
              findings={list.read}
              projects={projects}
              onOpen={setFinding}
              page={list.page}
              sorting={list.sorting}
              onPage={list.setPage}
              onSorting={list.setSorting}
              emptyHint={emptyHint}
              loadingLabel={loadingLabel}
            />
          ) : (
            <FindingGroupsTable
              by={filters.group}
              groups={groups.read}
              projects={projects}
              page={groups.page}
              sorting={groups.sorting}
              onPage={groups.setPage}
              onSorting={groups.setSorting}
              onDrill={onDrill}
              emptyHint={emptyHint}
              loadingLabel={loadingLabel}
            />
          )}
        </div>
      </Card>
      <FindingDrawer
        id={openFinding}
        onClose={() => setFinding(null)}
        pickers={pickers.data}
        explainSelected={securitySelected}
        onChanged={() => {
          list.read.reload();
          groups.read.reload();
          facets.reload();
          pickers.reload();
          status.reload();
        }}
      />
    </div>
  );
}
