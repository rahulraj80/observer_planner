// sessionLineage: the ONE view model behind the session drawer's lineage
// banner (shared/components/sessiondetail/LineageBanner.tsx), for both
// dashboards. The node knows a session's fork / sub-agent kind, its children
// and whether the parent is in its own database; the org server ships only
// the opaque parent pointer (rollup ParentThreadID, LINEAGE-1). Each app
// resolves its own row into LineageInput and this model decides the words.
//
// Pure (no React, no fetch) so it runs under node --test.

/** LineageKind is how a session relates to its parent. "parent" is the
 *  neutral case: a parent pointer with no recorded kind (the org wire). */
export type LineageKind = "subagent" | "fork" | "parent";

/** LineageChild is one session spawned from this one (node only). `meta` is
 *  the caller's pre-formatted volume ("12k tok", "3 actions"). */
export type LineageChild = { id: string; kind: "subagent" | "fork"; meta?: string };

export type LineageInput = {
  parentId?: string;
  parentKind?: LineageKind;
  /** parentOpenable: the caller can navigate to the parent. */
  parentOpenable?: boolean;
  /** parentClosedReason: shown when the parent can not be opened. */
  parentClosedReason?: string;
  children?: readonly LineageChild[];
  /** sidechainCount: inline sub-agent actions (no separate child rows). */
  sidechainCount?: number;
};

export type LineageView = {
  /** show: false when the session has no lineage at all. */
  show: boolean;
  parent: null | { id: string; label: string; kind: LineageKind; openable: boolean; title: string };
  /** sidechainNote: only when there is nothing else to show. */
  sidechainCount: number;
  childrenSummary: string;
};

/** shortSid is the drawer's short session id (the last `:agent:` segment,
 *  8 characters). */
export function shortSid(id: string): string {
  // Index form, not .at(-1): shared/ is type-checked under webcloud's ES2020
  // lib as well, where Array.prototype.at does not exist.
  const parts = id.split(":agent:");
  const tail = parts[parts.length - 1] ?? id;
  return tail.length > 8 ? tail.slice(0, 8) : tail;
}

const PARENT_LABEL: Record<LineageKind, string> = {
  subagent: "Subagent of",
  fork: "Forked from",
  parent: "Started from",
};

/** lineageView resolves one session's lineage into the banner's words. */
export function lineageView(input: LineageInput): LineageView {
  const parentId = (input.parentId ?? "").trim();
  const children = input.children ?? [];
  const sidechainCount = input.sidechainCount ?? 0;
  const show = parentId !== "" || children.length > 0 || sidechainCount > 0;
  let parent: LineageView["parent"] = null;
  if (parentId) {
    const kind = input.parentKind ?? "parent";
    const openable = Boolean(input.parentOpenable);
    parent = {
      id: parentId,
      kind,
      label: `${PARENT_LABEL[kind]} ${shortSid(parentId)}`,
      openable,
      title: openable ? `Open parent session ${parentId}` : (input.parentClosedReason ?? "Session navigation unavailable here"),
    };
  }
  let childrenSummary = "";
  if (children.length > 0) {
    const noun = children.every((c) => c.kind === "subagent")
      ? "subagent"
      : children.some((c) => c.kind === "subagent")
        ? "subagent/fork"
        : "fork";
    childrenSummary = `Spawned ${children.length} ${noun} ${children.length === 1 ? "session" : "sessions"}`;
  }
  return { show, parent, sidechainCount, childrenSummary };
}
