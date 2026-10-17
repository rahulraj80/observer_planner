// profileAssign.ts - the PURE half of Settings > Profiles "Per-tool
// assignments" (T-369): `observer profile assign tool:NAME <profile>` as a
// dashboard control.
//
// The card renders GET /api/config/profile-assignments and writes with PUT
// (assign) / DELETE (remove) on the same route; the daemon validates the
// tool against the integration registry and the profile against the
// profile store (the CLI's own rules, one shared Go owner). This module only
// derives what the card shows: the assigned rows, the picker options and the
// one-line change preview the confirm step states before anything is
// written.

/** One entry of the registry tool vocabulary the picker offers. */
export interface ProfileAssignTool {
  id: string;
  /** The registry declares a proxy route Observer drives today. */
  proxied: boolean;
}

/** GET /api/config/profile-assignments (and every write's) body. */
export interface ProfileAssignmentsResponse {
  default: string;
  by_provider: Record<string, string>;
  by_tool: Record<string, string>;
  profile_names: string[];
  tools: ProfileAssignTool[];
  changed: boolean;
  /**
   * On a GET: the etag of the config.toml this table was read from. A write
   * carries it back as `?base_etag=` so a file changed since is refused with
   * 409 config_changed instead of overwritten (T-639).
   */
  config_etag?: string;
}

/** One assigned tool, as the card lists it. */
export interface ProfileAssignmentRow {
  tool: string;
  profile: string;
  /** Not in the advertised registry vocabulary (a hand-edited or retired id). */
  unknown: boolean;
  /** In the vocabulary but its traffic does not reach the proxy today. */
  notProxied: boolean;
}

/** The fallback a tool with no assignment of its own resolves through. */
export const PROVIDER_FALLBACK = "its provider's profile";

/** The proxy-route caveat for a tool whose traffic is not proxied today. */
export const NOT_PROXIED_NOTE =
  "Not routed through the proxy today, so this assignment applies once its traffic reaches the proxy.";

/** assignmentTarget is the target spelling the daemon (and the CLI) take. */
export function assignmentTarget(tool: string): string {
  return `tool:${tool}`;
}

/** assignmentRows lists the per-tool assignments, sorted by tool id. */
export function assignmentRows(r: ProfileAssignmentsResponse | null): ProfileAssignmentRow[] {
  if (!r) return [];
  const known = new Map((r.tools ?? []).map((t) => [t.id, t]));
  return Object.entries(r.by_tool ?? {})
    .filter(([, profile]) => profile !== "")
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([tool, profile]) => {
      const t = known.get(tool);
      return { tool, profile, unknown: !t, notProxied: Boolean(t && !t.proxied) };
    });
}

/**
 * pickerTools is the "add" picker: every vocabulary tool that has no
 * assignment yet, proxied tools first (those are the ones an assignment
 * changes today), each alphabetical.
 */
export function pickerTools(r: ProfileAssignmentsResponse | null): ProfileAssignTool[] {
  if (!r) return [];
  const assigned = r.by_tool ?? {};
  return (r.tools ?? [])
    .filter((t) => !assigned[t.id])
    .sort((a, b) => (a.proxied === b.proxied ? a.id.localeCompare(b.id) : a.proxied ? -1 : 1));
}

/** toolOptionLabel is one picker option's text. */
export function toolOptionLabel(t: ProfileAssignTool): string {
  return t.proxied ? t.id : `${t.id} (not proxied)`;
}

/**
 * changePreview is the one-line diff the confirm step states before the
 * write: what the tool resolves to now, and what it will resolve to.
 * `next` empty means "remove the assignment".
 */
export function changePreview(r: ProfileAssignmentsResponse | null, tool: string, next: string): string {
  const current = r?.by_tool?.[tool] || PROVIDER_FALLBACK;
  return `${tool}: ${current} -> ${next || PROVIDER_FALLBACK}`;
}

/** isNoop reports whether assigning `next` to `tool` would change nothing. */
export function isNoop(r: ProfileAssignmentsResponse | null, tool: string, next: string): boolean {
  return (r?.by_tool?.[tool] ?? "") === next;
}
