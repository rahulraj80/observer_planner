import type { DoctorReport } from "./types.ts";

// toolDoctor.ts - the PURE half of Connected tools' per-tool "Run checks"
// (T-421 item 3): `observer doctor <tool>` as a dashboard action. The daemon
// runs the same report builder the CLI prints (GET /api/health/doctor/tool);
// this module names the route and phrases the result line.

/** GET /api/health/doctor/tool body: the doctor report scoped to one tool. */
export type ToolDoctorReport = DoctorReport & {
  tool: string;
  /** The terminal equivalent of the run, always present. */
  command: string;
};

/** toolDoctorPath is the route that runs one tool's checks. */
export function toolDoctorPath(tool: string): string {
  return `/api/health/doctor/tool?tool=${encodeURIComponent(tool)}`;
}

/** toolDoctorCommand is the CLI command the panel offers when it cannot run. */
export function toolDoctorCommand(tool: string): string {
  return `observer doctor ${tool}`;
}

/** toolDoctorSummary is the one-line outcome: counts, toned by the worst status. */
export function toolDoctorSummary(
  r: Pick<DoctorReport, "ok" | "warn" | "fail"> | null | undefined,
): { tone: "ok" | "warn" | "danger"; text: string } | null {
  if (!r) return null;
  const text = `${r.ok} ok, ${r.warn} warn, ${r.fail} fail`;
  if (r.fail > 0) return { tone: "danger", text };
  if (r.warn > 0) return { tone: "warn", text };
  return { tone: "ok", text: `${text}: all checks passed` };
}
