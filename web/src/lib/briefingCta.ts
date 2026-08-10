// Client for the briefing "Apply CTA" surface (Stage 2): simulate-first
// plans, one-click apply with stamped-snapshot undo, and the suspend
// (snooze/pause) + resume lifecycle. Every write is a Local-class API
// call; reads are View-class. Types mirror the Go handlers in
// internal/intelligence/dashboard/cta.go.
import { fetchJSON } from "@/lib/api";

export type CtaChange = {
  section: string;
  key: string;
  from: string;
  to: string;
  restart_required: boolean;
  change_id: string;
  already_active?: boolean;
};

export type AppliedLever = {
  change_id: string;
  section: string;
  key: string;
  from: string;
  to: string;
  backup?: string;
  stamp?: string;
  detector?: string;
  projected_monthly_usd?: number;
};

export type CtaSuspension = {
  dedup_key: string;
  detector?: string;
  title?: string;
  kind: "snooze" | "pause";
  until?: string;
  change_id?: string;
  section?: string;
  key?: string;
  from?: string;
  to?: string;
  backup?: string;
  time: string;
};

export type CtaPlan = {
  dedup_key: string;
  detector: string;
  title?: string;
  window_days: number;
  appliable: boolean;
  config_path_set: boolean;
  changes?: CtaChange[];
  savings_usd?: number;
  projected_monthly_usd?: number;
  evidence?: Record<string, number>;
  note?: string;
  applied?: boolean;
  applied_levers?: string[];
  suspended?: boolean;
  suspended_kind?: string;
  suspensions?: CtaSuspension[];
};

export type BriefingAction = {
  dedup_key: string;
  detector: string;
  category: string;
  severity: string;
  title: string;
  nudge: string;
  actionable: boolean;
  config_surface?: string;
  change_id?: string;
  from?: string;
  to?: string;
  restart_required: boolean;
  savings_usd?: number;
  projected_monthly_usd?: number;
  applied?: boolean;
  applied_levers?: AppliedLever[];
  suspended?: boolean;
  suspended_kind?: string;
  suspensions?: CtaSuspension[];
};

export type ActionsRailResponse = {
  date: string;
  basis: string;
  window: string;
  actions: BriefingAction[];
};

export type ApplyResponse = {
  applied: boolean;
  change: CtaChange;
  backup: string;
  dedup_key: string;
  projected_monthly_usd: number;
  restart_required: boolean;
  note?: string;
  ledger_error?: string;
};

export type SuspendResponse = {
  suspended: boolean;
  kind: "snooze" | "pause";
  dedup_key: string;
  until?: string;
  restored?: { change_id: string; section: string; key: string; from: string; to: string }[];
  note?: string;
  ledger_error?: string;
};

export type ResumeResponse = {
  resumed: number;
  dedup_key: string;
  details?: { change_id?: string; kind: string; re_applied?: boolean; backup?: string }[];
  ledger_error?: string;
};

export type SuspensionsResponse = {
  suspensions: CtaSuspension[];
  path: string;
};

export function fetchCtaPlan(dedupKey: string, window?: string): Promise<CtaPlan> {
  return fetchJSON<CtaPlan>("/api/briefing/cta/plan", {
    window: window || "",
  }, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ dedup_key: dedupKey, window: window || "" }),
  });
}

export function applyCta(dedupKey: string, changeId: string, window?: string): Promise<ApplyResponse> {
  return fetchJSON<ApplyResponse>("/api/briefing/cta/apply", undefined, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ dedup_key: dedupKey, change_id: changeId, window: window || "" }),
  });
}

export function suspendCta(
  dedupKey: string,
  opts: { until?: string; change_id?: string; detector?: string; title?: string } = {},
): Promise<SuspendResponse> {
  return fetchJSON<SuspendResponse>("/api/briefing/cta/suspend", undefined, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ dedup_key: dedupKey, ...opts }),
  });
}

export function resumeCta(dedupKey: string, changeId?: string): Promise<ResumeResponse> {
  return fetchJSON<ResumeResponse>("/api/briefing/cta/resume", undefined, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ dedup_key: dedupKey, change_id: changeId || "" }),
  });
}

export function revertCta(section: string, key: string): Promise<{ restored: boolean; from: string; to: string }> {
  return fetchJSON<{ restored: boolean; from: string; to: string }>("/api/briefing/cta/revert", undefined, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ section, key }),
  });
}

export function fetchActions(date?: string): Promise<ActionsRailResponse> {
  return fetchJSON<ActionsRailResponse>("/api/briefing/actions", { date: date || "" });
}

export function fetchSuspensions(): Promise<SuspensionsResponse> {
  return fetchJSON<SuspensionsResponse>("/api/briefing/suspensions");
}

// humanizeLever maps a config lever key to a short action label.
export function leverLabel(key: string): string {
  if (key === "mode") return "Enforce routing";
  if (key === "policy") return "Adopt policy";
  if (key === "stickiness.min_turns_between_switches") return "Lower coherence floor";
  return key;
}
