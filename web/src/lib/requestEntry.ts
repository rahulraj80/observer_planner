// Entry points into the Requests page's new-request form (request-grants
// design §1.2 and G4). Each arrives as a query string and opens the form
// prefilled; nothing is ever sent until the developer presses Send.
//
//   /requests?event=<guard event id>      the Security page's "Request access"
//   /requests?rule=<rule>&session=<id>    the guard's org-locked deny line (G4)
//   /requests?new=budget[&budget=<id>]    a budget increase (T-522): the budget
//                                         banner, its "+N more" list, and the
//                                         guard's budget deny line. With no id
//                                         the form starts on the budget that
//                                         ran out; an id the node does not know
//                                         falls back the same way.
//   /requests?new=page[&page=<id>]        a hidden page or tab (T-435): the
//                                         Privacy page's "Pages managed by your
//                                         organization" card.
//   /requests?new=config[&setting=<id>]   an AI tool setting edit (T-436): the
//                                         AI tool settings page's read-only
//                                         notice and a locked setting's panel.
//   /requests?type=<request type>         e.g. type=budget_increase (older link)
//
// The deny line links rule + session rather than an event id because the
// vendor reply is written before the event is persisted.

/** The query parameters an entry point may carry; cleared once consumed. */
export const ENTRY_PARAMS = ["event", "rule", "session", "type", "new", "budget", "page", "setting", "component"] as const;

/** A draft the form opens with when the prefill call is unavailable. */
export type EntryDraft = {
  request_type: string;
  message: string;
  payload: Record<string, unknown>;
};

export type RequestEntry =
  /** Ask /api/requests/prefill with `query`; open `fallback` if that fails. */
  | { kind: "prefill"; query: Record<string, string>; fallback: EntryDraft }
  /** Open an empty draft of this type. */
  | { kind: "type"; draft: EntryDraft }
  | null;

// A rule id is short; anything longer or carrying control characters is not
// one (the server refuses the same, 400).
const RULE_MAX = 64;
const SESSION_MAX = 256;
// A budget id is an org-minted identifier; the form only ever matches it
// against the budgets the node already holds, so a bounded clean string is
// enough here.
const BUDGET_MAX = 128;
const clean = (v: string | null, max: number): string => {
  const t = (v ?? "").trim();
  // eslint-disable-next-line no-control-regex
  return t.length <= max && !/[\u0000-\u001f\u007f]/.test(t) ? t : "";
};

/** The `new=` values and the request type each opens. */
const NEW_KINDS: Record<string, string> = {
  budget: "budget_increase",
  page: "page_access",
  config: "config_edit",
  component: "component_enable",
};

// A component id ("codeintel", "adapter.claude-code") is short and plain.
const COMPONENT_MAX = 96;

/** componentRequestLink opens a component-access request (lane MP-GOV): the
 *  "Request from your organization" path of a component a managed node's
 *  policy leaves inert. */
export function componentRequestLink(id?: string): string {
  const c = clean(id ?? "", COMPONENT_MAX);
  return c ? `/requests?new=component&component=${encodeURIComponent(c)}` : "/requests?new=component";
}

// An AI tool setting id ("codex/model", "claude-code/permissions.defaultMode")
// or "<tool>/*".
const SETTING_MAX = 160;

/** configEditRequestLink opens an AI tool setting edit request (T-436). */
export function configEditRequestLink(setting?: string): string {
  const s = clean(setting ?? "", SETTING_MAX);
  return s ? `/requests?new=config&setting=${encodeURIComponent(s)}` : "/requests?new=config";
}

// A page or tab id ("security" or "security/rules") is short and plain.
const PAGE_MAX = 64;

/** pageRequestLink opens a page-access request (T-435) on page `id`. */
export function pageRequestLink(id?: string): string {
  const p = clean(id ?? "", PAGE_MAX);
  return p ? `/requests?new=page&page=${encodeURIComponent(p)}` : "/requests?new=page";
}

/** budgetRequestLink is the stable deep link that opens a budget-increase
 *  request (T-522): on `budgetId` when given, else on the budget that ran out. */
export function budgetRequestLink(budgetId?: string): string {
  const id = clean(budgetId ?? "", BUDGET_MAX);
  return id ? `/requests?new=budget&budget=${encodeURIComponent(id)}` : "/requests?new=budget";
}

/** requestEntryFrom reads the entry point a URL carries, or null. */
export function requestEntryFrom(params: URLSearchParams): RequestEntry {
  const ev = (params.get("event") ?? "").trim();
  if (/^[1-9][0-9]*$/.test(ev)) {
    return {
      kind: "prefill",
      query: { event_id: ev },
      fallback: { request_type: "guard_exemption", message: "", payload: { breadth: "exact" } },
    };
  }
  const rule = clean(params.get("rule"), RULE_MAX);
  if (rule) {
    const session = clean(params.get("session"), SESSION_MAX);
    const query: Record<string, string> = { rule };
    if (session) query.session = session;
    return {
      kind: "prefill",
      query,
      fallback: { request_type: "guard_exemption", message: "", payload: { rule_id: rule, breadth: "rule" } },
    };
  }
  const type = NEW_KINDS[(params.get("new") ?? "").trim()] ?? (params.get("type") ?? "").trim();
  if (type) {
    const payload: Record<string, unknown> = {};
    const budget = clean(params.get("budget"), BUDGET_MAX);
    if (type === "budget_increase" && budget) payload.budget_id = budget;
    const page = clean(params.get("page"), PAGE_MAX);
    if (type === "page_access" && page) payload.target = page;
    const setting = clean(params.get("setting"), SETTING_MAX);
    if (type === "config_edit" && setting) payload.target = setting;
    const component = clean(params.get("component"), COMPONENT_MAX);
    if (type === "component_enable" && component) payload.target = component;
    return { kind: "type", draft: { request_type: type, message: "", payload } };
  }
  return null;
}

/** withoutEntryParams drops the consumed entry parameters, keeping the rest. */
export function withoutEntryParams(params: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(params);
  for (const k of ENTRY_PARAMS) next.delete(k);
  return next;
}
