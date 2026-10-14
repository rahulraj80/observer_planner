// plans: the Plans & limits page's wire types and its vocabulary (the words
// for every code the server sends). Pure (no React), so the node test
// runner covers it. The server sends codes, never sentences; this file owns
// the English. internal/planlimits/vocab_source_test.go fails when a
// provider, source, status, kind or unit the Go registry knows is missing
// here.

import { COST_HIDDEN, dollarsShown, fmtRelative, fmtUSD } from "../../../shared/lib/format.ts";

// ---------- wire types (GET /api/plans) ----------

export type PlanPace = { projected_frac: number; runs_out_at?: string };

export type PlanWindow = {
  key: string;
  unit: string;
  used?: number;
  limit?: number;
  remaining?: number;
  used_frac?: number;
  resets_at?: string;
  length_ns?: number;
  rolling?: boolean;
  source: string;
  source_kind: string;
  rung: string;
  confidence: string;
  observed_at: string;
  expired?: boolean;
  pace?: PlanPace;
};

export type PlanBalance = {
  kind: string;
  unit: string;
  amount: number;
  source: string;
  source_kind: string;
  rung: string;
  confidence: string;
  observed_at: string;
};

export type PlanSpend = { period: string; unit: string; amount: number; scope: string };

export type PlanRunState = {
  status: string;
  failures: number;
  shape_failures: number;
  next_due: string;
  stopped: boolean;
};

export type PlanCard = {
  id: string;
  provider: string;
  account_id?: string;
  label?: string;
  scope_reason?: string;
  sources: string[];
  experimental?: boolean;
  status: string;
  health: string;
  plan_code?: string;
  plan_label?: string;
  observed_at?: string;
  windows: PlanWindow[];
  balances: PlanBalance[];
  spend: PlanSpend[];
  state?: PlanRunState;
};

export type PlanAccount = {
  id: string;
  provider: string;
  source: string;
  label: string;
  params?: Record<string, string>;
  secrets?: string[];
  enabled: boolean;
  state: PlanRunState;
  created_at: string;
};

export type CatalogSecret = { key: string; required: boolean };
export type CatalogParam = { key: string; required: boolean; choices?: string[]; numeric?: boolean };
export type CatalogRead = { base: string; path: string; item?: string };

export type CatalogSource = {
  id: string;
  kind: string;
  rung: string;
  experimental: boolean;
  passive: boolean;
  secrets: CatalogSecret[];
  params: CatalogParam[];
  command?: string;
  doc_url?: string;
  reads?: CatalogRead[];
};

export type CatalogProvider = {
  id: string;
  group: string;
  has_experimental: boolean;
  sources: CatalogSource[];
};

export type PlansView = {
  generated_at: string;
  cards: PlanCard[];
  accounts: PlanAccount[];
  opt_ins: string[];
  keychain: string;
};

export type PlansResponse = {
  view: PlansView;
  catalog: CatalogProvider[];
  confirm_token?: string;
};

export type PlanRefreshResult = {
  account_id: string;
  outcome: string;
  detail?: string;
  held?: boolean;
};

// ---------- vocabulary ----------

export type Tone = "neutral" | "success" | "warn" | "danger" | "info" | "accent";

/** PROVIDER_LABELS: every provider id in internal/planlimits/registry.go. */
export const PROVIDER_LABELS: Record<string, string> = {
  claude: "Claude",
  chatgpt: "ChatGPT and Codex",
  github_copilot: "GitHub Copilot",
  cursor: "Cursor",
  windsurf: "Windsurf",
  kiro: "Kiro",
  kimi_code: "Kimi Code",
  amp: "Amp",
  augment: "Augment",
  openrouter: "OpenRouter",
  deepseek: "DeepSeek",
  moonshot: "Moonshot",
  xai: "xAI",
  other: "Other",
};

export const GROUP_LABELS: Record<string, string> = {
  plans: "Coding plans",
  balance: "API balances",
};

export function providerLabel(id: string): string {
  return PROVIDER_LABELS[id] ?? "Other";
}

/** SOURCE_LABELS names each way to read a provider, as the method picker shows it. */
export const SOURCE_LABELS: Record<string, string> = {
  "claude.proxy": "Your Claude traffic",
  "claude.statusline": "Claude Code status line",
  "chatgpt.logs": "Codex logs",
  "chatgpt.appserver": "Codex app on this machine",
  "openrouter.api": "API key",
  "deepseek.api": "API key",
  "moonshot.api": "API key",
  "xai.management": "Management key",
  "github_copilot.billing": "GitHub token",
  "kiro.cli": "Kiro CLI",
  "amp.cli": "Amp CLI",
  "augment.cli": "Auggie CLI",
  manual: "Type it in",
  "claude.oauth": "Claude Code sign-in",
  "chatgpt.web_usage": "Codex sign-in",
  "cursor.app": "Cursor sign-in",
  "windsurf.cache": "Windsurf saved plan",
  "kimi_code.api": "Kimi Code key",
};

/** SOURCE_HOWTO is the one-line instruction under each method. */
export const SOURCE_HOWTO: Record<string, string> = {
  "claude.proxy": "Read from your Claude Code traffic through Observer. Nothing to add.",
  "claude.statusline": "Read from the Claude Code status line when Observer provides it. Nothing to add.",
  "chatgpt.logs": "Read from the limits Codex writes to its own logs. Nothing to add.",
  "chatgpt.appserver": "Asks the Codex app on this machine for your limits. Codex must be signed in.",
  "openrouter.api": "Paste an OpenRouter API key. Read-only. A management key also shows credits.",
  "deepseek.api": "Paste a DeepSeek API key. Observer reads your balance only.",
  "moonshot.api": "Paste a Moonshot API key. Observer reads your balance only.",
  "xai.management": "Paste an xAI management key and your team ID. Observer reads balance and spend.",
  "github_copilot.billing": "Paste a GitHub token with read access to Plan, and your GitHub username.",
  "kiro.cli": "Runs kiro-cli on this machine to read your usage. Kiro must be signed in.",
  "amp.cli": "Runs amp on this machine to read your usage. Amp must be signed in.",
  "augment.cli": "Runs auggie on this machine to read your usage. Auggie must be signed in.",
  manual: "Type your plan's allowance. Observer estimates use from what it records.",
  "claude.oauth": "Reads Claude Code's own sign-in on this machine, read-only, to ask Anthropic for your limits.",
  "chatgpt.web_usage": "Reads Codex's own sign-in on this machine, read-only, to ask ChatGPT for your limits.",
  "cursor.app": "Reads Cursor's own sign-in on this machine, read-only, to ask Cursor for your usage.",
  "windsurf.cache": "Reads the plan details Windsurf saved on this machine. No network call.",
  "kimi_code.api": "Paste a Kimi Code key. Uses an endpoint Kimi has not documented.",
};

/** The one honest warning next to every experimental switch. */
export const EXPERIMENTAL_WARNING =
  "Experimental: uses another app's sign-in or an undocumented endpoint. It can break, and the vendor's terms may not allow it.";

export const SECRET_LABELS: Record<string, string> = {
  key: "API key",
  mgmt: "Management key",
  pat: "GitHub token",
};

export const PARAM_LABELS: Record<string, string> = {
  region: "Region",
  team_id: "Team ID",
  username: "GitHub username",
  allowance: "Allowance",
  plan: "Plan name",
  unit: "Measured in",
  used: "Used so far",
  renews_day: "Renews on day of month",
  window: "Resets",
};

export const CHOICE_LABELS: Record<string, string> = {
  global: "Global",
  china: "China",
  usd: "Dollars",
  requests: "Requests",
  credits: "Credits",
  percent: "Percent",
  monthly: "Every month",
  weekly: "Every week",
  daily: "Every day",
};

/** SOURCE_KIND_LABELS: where a number came from, for the card header. */
export const SOURCE_KIND_LABELS: Record<string, string> = {
  official_api: "Official API",
  admin_api: "Official API",
  status_line: "From the status line",
  vendor_rpc: "From the Codex app",
  vendor_cli: "From the vendor's CLI",
  passive_proxy: "From your traffic",
  passive_log: "From Codex logs",
  manual: "Estimated",
  local_state: "Experimental",
  borrowed_token: "Experimental",
  undocumented: "Experimental",
  browser: "Experimental",
};

export type StatusVocab = { label: string; tone: Tone };

/** STATUS_VOCAB: every status code; "ok" renders no pill. */
export const STATUS_VOCAB: Record<string, StatusVocab> = {
  ok: { label: "", tone: "neutral" },
  throttled: { label: "Limit reached", tone: "danger" },
  plan_only: { label: "No usage meter", tone: "neutral" },
  unmetered: { label: "No metered limit", tone: "neutral" },
  billed_elsewhere: { label: "Billed elsewhere", tone: "neutral" },
  auth_needed: { label: "Sign in again", tone: "warn" },
  error: { label: "Last check failed", tone: "warn" },
  endpoint_changed: { label: "Vendor changed its API", tone: "warn" },
  stale: { label: "Out of date", tone: "neutral" },
  not_opted_in: { label: "Experimental is off", tone: "neutral" },
  skipped: { label: "Not needed", tone: "neutral" },
  not_applicable: { label: "Not used", tone: "neutral" },
  unavailable: { label: "Not available", tone: "neutral" },
};

export function statusVocab(code: string): StatusVocab {
  return STATUS_VOCAB[code] ?? { label: "Unknown", tone: "neutral" };
}

/** SCOPE_NOTES: why a passive card is not tied to one login. */
export const SCOPE_NOTES: Record<string, string> = {
  bound: "",
  explicit: "",
  no_session: "Account not identified",
  no_observation: "Account not identified",
  conflict: "More than one account was used",
};

export const UNITS = ["percent", "credits", "usd_micros", "cny_micros", "requests", "tokens", "ai_credits"] as const;

const WINDOW_LABELS: Record<string, string> = {
  "5h": "5-hour",
  daily: "Daily",
  weekly: "Weekly",
  monthly: "Monthly",
  cycle: "This cycle",
  key_cap: "Key cap",
  spend_limit: "Spend limit",
  extra_usage: "Extra usage",
  free_daily_requests: "Free requests today",
  bonus: "Bonus credits",
  premium: "Premium requests",
  ai_credit: "AI credits",
};

const PREFIX_LABELS: Record<string, string> = {
  model: "Weekly",
  cycle: "This cycle",
  daily: "Daily",
};

function titleWord(s: string): string {
  const t = s.replace(/[_-]+/g, " ").trim();
  return t ? t[0].toUpperCase() + t.slice(1) : "";
}

/** windowLabel names a window key: "5h" -> "5-hour", "model:opus" -> "Weekly, Opus". */
export function windowLabel(key: string): string {
  if (WINDOW_LABELS[key]) return WINDOW_LABELS[key];
  const at = key.indexOf(":");
  if (at > 0) {
    const prefix = key.slice(0, at);
    const rest = titleWord(key.slice(at + 1));
    const head = PREFIX_LABELS[prefix] ?? titleWord(prefix);
    return rest ? `${head}, ${rest}` : head;
  }
  return titleWord(key) || "Usage";
}

const BALANCE_LABELS: Record<string, string> = {
  prepaid: "Prepaid",
  granted: "Granted",
  topup: "Topped up",
  voucher: "Voucher",
  cash: "Cash",
  total: "Balance",
  posted: "Balance",
  credits: "Credits",
  workspace_pool: "Workspace pool",
  included_used: "Included, used",
};

export function balanceLabel(kind: string): string {
  return BALANCE_LABELS[kind] ?? titleWord(kind);
}

const PERIOD_LABELS: Record<string, string> = {
  day: "today",
  week: "this week",
  month: "this month",
  cycle: "this cycle",
  all_time: "in total",
};

export function spendLabel(s: PlanSpend): string {
  return `Spent ${PERIOD_LABELS[s.period] ?? ""}`.trim();
}

const PLAN_CODE_LABELS: Record<string, string> = {
  payg: "Pay as you go",
  free: "Free",
  prepaid: "Prepaid",
  pro: "Pro",
  max: "Max",
  max_5x: "Max 5x",
  max_20x: "Max 20x",
  team: "Team",
  enterprise: "Enterprise",
  plus: "Plus",
  business: "Business",
};

/** planName: the vendor's own plan wording first, then a known code. */
export function planName(c: Pick<PlanCard, "plan_label" | "plan_code">): string {
  if (c.plan_label) return c.plan_label;
  if (!c.plan_code) return "";
  return PLAN_CODE_LABELS[c.plan_code] ?? titleWord(c.plan_code);
}

/** cardTitle: "Claude Max (work)", "OpenRouter", "Cursor Pro". */
export function cardTitle(c: PlanCard): string {
  const plan = planName(c);
  const head = plan ? `${providerLabel(c.provider)} ${plan}` : providerLabel(c.provider);
  return c.label ? `${head} (${c.label})` : head;
}

// ---------- numbers ----------

const cnyFmt = new Intl.NumberFormat("en-US", { style: "currency", currency: "CNY" });

/** fmtAmount renders one amount in its unit. Money follows the cost display. */
export function fmtAmount(unit: string, n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return "-";
  switch (unit) {
    case "usd_micros":
      return fmtUSD(n / 1e6);
    case "cny_micros":
      return dollarsShown() ? cnyFmt.format(n / 1e6) : COST_HIDDEN;
    case "percent":
      return `${Math.round(n)}%`;
    case "credits":
      return `${fmtNum(n)} credits`;
    case "ai_credits":
      return `${fmtNum(n)} AI credits`;
    case "requests":
      return `${fmtNum(n)} requests`;
    case "tokens":
      return `${fmtNum(n)} tokens`;
    default:
      return fmtNum(n);
  }
}

function fmtNum(n: number): string {
  return Number.isInteger(n) ? n.toLocaleString("en-US") : n.toLocaleString("en-US", { maximumFractionDigits: 2 });
}

/** isMoney: a unit that is hidden in the token-usage cost display. */
export function isMoney(unit: string): boolean {
  return unit === "usd_micros" || unit === "cny_micros";
}

/** windowFrac is the bar fill, or null when the window has no fraction (never 0). */
export function windowFrac(w: PlanWindow): number | null {
  if (w.used_frac != null && Number.isFinite(w.used_frac)) return w.used_frac;
  if (w.used != null && w.limit != null && w.limit > 0) return w.used / w.limit;
  return null;
}

/** windowTone colours a bar by how close it is to the limit. */
export function windowTone(frac: number | null): Tone {
  if (frac == null) return "neutral";
  if (frac >= 0.9) return "danger";
  if (frac >= 0.7) return "warn";
  return "accent";
}

/** windowValue: "52%", "~64%", "$9.10 of $30.00", "120 of 500 requests". */
export function windowValue(w: PlanWindow): string {
  const approx = w.confidence === "estimated" ? "~" : "";
  if (w.unit !== "percent" && w.used != null && w.limit != null) {
    if (isMoney(w.unit) && !dollarsShown()) {
      const f = windowFrac(w);
      return f == null ? COST_HIDDEN : `${approx}${Math.round(f * 100)}%`;
    }
    return `${approx}${fmtAmount(w.unit, w.used)} of ${fmtAmount(w.unit, w.limit)}`;
  }
  const f = windowFrac(w);
  if (f != null) return `${approx}${Math.round(f * 100)}%`;
  if (w.remaining != null) return `${fmtAmount(w.unit, w.remaining)} left`;
  if (w.limit != null) return `${fmtAmount(w.unit, w.limit)} cap`;
  return "Not reported";
}

/** resetText: "resets in 2 hours", "resets Sep 12, 2026". */
export function resetText(w: PlanWindow, now: number = Date.now()): string {
  if (w.expired) return "reset since last reading";
  if (!w.resets_at) return "";
  return `resets ${fmtRelative(w.resets_at, now)}`;
}

/** paceText: one line about the pace, only when it says something. */
export function paceText(w: PlanWindow, now: number = Date.now()): string {
  const p = w.pace;
  if (!p) return "";
  if (p.runs_out_at) return `At this pace it runs out ${fmtRelative(p.runs_out_at, now)}`;
  if (p.projected_frac < 1) return "At this pace it lasts until the reset";
  return "";
}

/** healthText: the freshness note in a card header. */
export function healthText(c: PlanCard, now: number = Date.now()): string {
  if (c.health === "waiting") return "Waiting for the first reading";
  if (!c.observed_at) return "";
  return `Updated ${fmtRelative(c.observed_at, now)}`;
}

/** cardSourceText: where the card's numbers came from, deduplicated. */
export function cardSourceText(c: PlanCard): string {
  const kinds: string[] = [];
  for (const w of c.windows) kinds.push(SOURCE_KIND_LABELS[w.source_kind] ?? "");
  for (const b of c.balances) kinds.push(SOURCE_KIND_LABELS[b.source_kind] ?? "");
  const uniq = Array.from(new Set(kinds.filter(Boolean)));
  if (uniq.length > 0) return uniq.join(", ");
  return c.sources.map((s) => SOURCE_LABELS[s] ?? "").filter(Boolean).join(", ");
}

/** methodOptions: the sources an account can use for a provider (never a passive one). */
export function methodOptions(p: CatalogProvider | undefined): CatalogSource[] {
  return (p?.sources ?? []).filter((s) => !s.passive);
}

/** canSave: every required field of the chosen source is filled. */
export function canSave(src: CatalogSource | undefined, params: Record<string, string>, secrets: Record<string, string>): boolean {
  if (!src) return false;
  for (const s of src.secrets) if (s.required && !(secrets[s.key] ?? "").trim()) return false;
  for (const p of src.params) if (p.required && !(params[p.key] ?? "").trim()) return false;
  for (const p of src.params) {
    const v = (params[p.key] ?? "").trim();
    if (p.numeric && v !== "" && !Number.isFinite(Number(v))) return false;
  }
  return true;
}

/** errorText strips the server's JSON envelope from an error reply. */
export function errorText(body: string): string {
  try {
    const j = JSON.parse(body) as { error?: string };
    if (j.error) return j.error[0].toUpperCase() + j.error.slice(1);
  } catch {
    // not JSON
  }
  return body.trim() || "The request failed";
}
