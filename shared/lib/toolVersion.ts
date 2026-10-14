// toolVersion - the one reading of a session's two captured tool builds
// (T-784): `tool_version` is the build the session started on (first seen,
// never moves) and `tool_version_last` the latest build seen in it. An older
// node or org server sends no `tool_version_last`; it then means "same as
// tool_version". Pure, so the node dashboard (web/) and the org dashboard
// (web2/) render an upgrade with the same rule and the same words.

/** ToolVersionView is what a session header shows. */
export type ToolVersionView = {
  /** The build to show as the session's version ("" when unknown). */
  current: string;
  /** The build the session started on, set only when it was upgraded. */
  upgradedFrom: string;
  /** One plain sentence for a tooltip ("" when there is nothing to say). */
  note: string;
};

/**
 * toolVersionView reads a session's first and last builds. `last` falls back
 * to `first`; when they differ the session was upgraded mid-session and the
 * latest build is the one shown.
 */
export function toolVersionView(first?: string | null, last?: string | null): ToolVersionView {
  const f = (first ?? "").trim();
  const l = (last ?? "").trim() || f;
  if (!l) return { current: "", upgradedFrom: "", note: "" };
  if (f && f !== l) {
    return { current: l, upgradedFrom: f, note: `Upgraded from ${f} to ${l} in this session` };
  }
  return { current: l, upgradedFrom: "", note: "Tool version" };
}
