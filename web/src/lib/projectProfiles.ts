// projectProfiles.ts - the PURE half of Settings > Profiles "Per-project
// assignments" (T-421 item 1): `observer config set --project <root>
// profiles.*` as a dashboard control.
//
// The card renders GET /api/config/project-profiles and writes with PUT
// (assign) / DELETE (remove) on the same route, always in two steps: a
// preview ({preview: true}) that returns the exact file diff, then an apply
// that echoes the preview's base_sha (the daemon refuses an apply without one,
// and refuses one whose file changed since). The daemon validates the project
// against the roots it knows, the target with the CLI's grammar and the
// profile against the profile store. This module only derives what the card
// shows.

import type { ProfileAssignTool } from "./profileAssign";

/** One known project and what its .observer/config.toml assigns. */
export interface ProjectProfileRow {
  root: string;
  has_file: boolean;
  default?: string;
  by_provider?: Record<string, string>;
  by_tool?: Record<string, string>;
  /** The file exists but does not parse (the daemon ignores it until fixed). */
  error?: string;
  /** Why this project's file cannot be written from the dashboard. */
  refusal?: string;
}

/** GET /api/config/project-profiles body. */
export interface ProjectProfilesResponse {
  projects: ProjectProfileRow[];
  truncated: boolean;
  profile_names: string[];
  providers: string[];
  tools: ProfileAssignTool[];
  global_default: string;
}

/** A preview's (or apply's) body. */
export interface ProjectProfileEdit {
  root: string;
  path: string;
  key: string;
  existed: boolean;
  changed: boolean;
  before: string;
  after: string;
  diff: string;
  base_sha: string;
  applied: boolean;
  project?: ProjectProfileRow;
}

/** One assignment a project file makes. */
export interface ProjectAssignment {
  /** The target spelling the daemon (and the CLI's grammar) take. */
  target: string;
  label: string;
  profile: string;
}

/** The project-wide target: every request from this project. */
export const PROJECT_DEFAULT_TARGET = "default";

/** targetLabel is how the card names a target. */
export function targetLabel(target: string): string {
  if (target === PROJECT_DEFAULT_TARGET) return "all traffic";
  if (target.startsWith("tool:")) return target.slice("tool:".length);
  return `${target} traffic`;
}

/** projectAssignments lists what one project's file assigns, default first. */
export function projectAssignments(row: ProjectProfileRow | null | undefined): ProjectAssignment[] {
  if (!row) return [];
  const out: ProjectAssignment[] = [];
  if (row.default) {
    out.push({ target: PROJECT_DEFAULT_TARGET, label: targetLabel(PROJECT_DEFAULT_TARGET), profile: row.default });
  }
  for (const [k, v] of Object.entries(row.by_provider ?? {}).sort(([a], [b]) => a.localeCompare(b))) {
    if (v) out.push({ target: k, label: targetLabel(k), profile: v });
  }
  for (const [k, v] of Object.entries(row.by_tool ?? {}).sort(([a], [b]) => a.localeCompare(b))) {
    if (v) out.push({ target: `tool:${k}`, label: targetLabel(`tool:${k}`), profile: v });
  }
  return out;
}

/** assignedProjects is every project whose file assigns at least one profile. */
export function assignedProjects(r: ProjectProfilesResponse | null): ProjectProfileRow[] {
  return (r?.projects ?? []).filter((p) => projectAssignments(p).length > 0);
}

/** targetOptions is the target picker: project-wide, then providers, then tools. */
export function targetOptions(r: ProjectProfilesResponse | null): { value: string; label: string }[] {
  if (!r) return [];
  return [
    { value: PROJECT_DEFAULT_TARGET, label: "All traffic from this project" },
    ...(r.providers ?? []).map((p) => ({ value: p, label: `${p} traffic` })),
    ...(r.tools ?? []).map((t) => ({ value: `tool:${t.id}`, label: t.proxied ? t.id : `${t.id} (not proxied)` })),
  ];
}

/** projectName is the last path segment of a root (the card's short label). */
export function projectName(root: string): string {
  const parts = root.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] || root;
}

/**
 * changeSummary is the one-line statement the confirm step makes: what the
 * target resolves to now in this project, and what it will resolve to.
 * An empty `next` means "remove the project's assignment".
 */
export function changeSummary(row: ProjectProfileRow | null | undefined, target: string, next: string): string {
  const current = projectAssignments(row).find((a) => a.target === target)?.profile;
  const from = current || "the global assignment";
  const to = next || "the global assignment";
  return `${row ? projectName(row.root) : "project"}, ${targetLabel(target)}: ${from} -> ${to}`;
}
