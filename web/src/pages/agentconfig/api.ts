// The AI tool settings page's write calls. Reads go through useApi; the
// preview POSTs are read-shaped on the server (nothing written) and the
// saves carry the owner-local confirm token (X-Observer-Confirm), which
// lib/api.ts refreshes from the cookie at send time.

import { ApiError, fetchJSON } from "@/lib/api";
import {
  errorBody,
  type AgentConfigErrorBody,
  type CommonPreview,
  type CommonRequest,
  type Preview,
  type SaveOutcome,
  type SaveRequest,
} from "@/lib/agentConfig";

const post = (body: unknown, confirmToken?: string): RequestInit => ({
  method: "POST",
  headers: {
    "Content-Type": "application/json",
    ...(confirmToken !== undefined ? { "X-Observer-Confirm": confirmToken } : {}),
  },
  body: JSON.stringify(body),
});

/** A failed call: the structured body when the server sent one. */
export type CallError = { status: number; body: AgentConfigErrorBody | null; message: string };

export function asCallError(e: unknown): CallError {
  if (e instanceof ApiError) {
    const body = errorBody(e.body);
    return { status: e.status, body, message: body?.message || e.message };
  }
  return { status: 0, body: null, message: e instanceof Error ? e.message : String(e) };
}

export function previewSave(req: SaveRequest): Promise<Preview> {
  return fetchJSON<Preview>("/api/agent-config/preview", undefined, post(req));
}

export function save(req: SaveRequest, confirmToken: string): Promise<SaveOutcome> {
  return fetchJSON<SaveOutcome>("/api/agent-config/save", undefined, post(req, confirmToken));
}

export function previewCommon(req: CommonRequest): Promise<CommonPreview> {
  return fetchJSON<CommonPreview>("/api/agent-config/common/preview", undefined, post(req));
}

export function saveCommon(
  req: CommonRequest,
  confirmToken: string,
): Promise<{ saved: boolean; written: SaveOutcome[] | null }> {
  return fetchJSON("/api/agent-config/common/save", undefined, post(req, confirmToken));
}

export function applyOrg(confirmToken: string): Promise<{ has_policy: boolean; version: number; writes: number; drift_events: number; targets: number }> {
  return fetchJSON("/api/agent-config/org/apply", undefined, post({}, confirmToken));
}

export function newInstructionPath(tool: string, fileId: string, name: string, project: string): Promise<{ path: string }> {
  return fetchJSON("/api/agent-config/instructions/new-path", {
    tool,
    file_id: fileId,
    name,
    project: project || undefined,
  });
}
