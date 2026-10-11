// The MCP package reputation control's pure model (T-496): the shapes of
// GET / POST /api/guard/mcp/reputation and the lines the card renders. The
// lookup itself is the server's notify.LookupNPMReputation, the function
// `observer guard mcp reputation` runs.

/** GET /api/guard/mcp/reputation. */
export type MCPReputationStatus = {
  cloud_enabled: boolean;
  reputation_enabled: boolean;
  available: boolean;
  registry: string;
  young_days: number;
  off_reason: string;
  confirm_token: string;
};

/** POST /api/guard/mcp/reputation (200). */
export type MCPReputationResult = {
  package: string;
  name: string;
  description?: string;
  latest: string;
  version_count: number;
  created_at?: string;
  modified_at?: string;
  age_days: number;
  maintainers: number;
  /** The CLI's own rendering, line for line. */
  lines: string[];
  young: boolean;
  warning?: string;
};

/** optInLine says what is still needed before a lookup can run, or "" when none is. */
export function optInLine(s: Pick<MCPReputationStatus, "cloud_enabled" | "reputation_enabled">): string {
  const missing: string[] = [];
  if (!s.cloud_enabled) missing.push("[guard.cloud] enabled = true");
  if (!s.reputation_enabled) missing.push("[guard.cloud.reputation] enabled = true");
  if (missing.length === 0) return "";
  return `Off until config.toml sets ${missing.join(" and ")}. Cloud features stay a hand-written config decision.`;
}

/** validPackageName mirrors the server's check: one non-empty name, no spaces, at most 214 characters. */
export function validPackageName(name: string): boolean {
  const n = name.trim();
  return n.length > 0 && n.length <= 214 && !/\s/.test(n);
}
