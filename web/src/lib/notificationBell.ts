// notificationBell - the pure half of the node dashboard's notification bell
// (D-323): the wire shape of GET /api/notifications (internal/notify/feed)
// and the decisions the TopBar bell renders. No JSX, no fetch: pinned by
// notificationBell.test.ts.
//
// Read / dismiss state is node-local on the daemon (internal/notify/
// feedstate), not in this browser, so a dismissal holds on every device that
// opens this dashboard.

export type NotificationSeverity = "critical" | "warn" | "info";

/** NotificationRow mirrors feed.Notification. */
export interface NotificationRow {
  id: string;
  kind: string;
  severity: NotificationSeverity;
  title: string;
  body?: string;
  section?: string;
  link?: string;
  at: string;
  /** A money figure the bell formats under the cost display; never in text. */
  usd?: number;
  read: boolean;
}

/** NotificationsResponse mirrors GET /api/notifications. */
export interface NotificationsResponse {
  notifications: NotificationRow[];
  unread: number;
  generated_at: string;
  /** The owner-local confirm token the state POST echoes (T-530). */
  confirm_token?: string;
}

/**
 * stateRequestInit is the POST /api/notifications/state request: the ids and
 * the action, with the JSON and owner-local confirm headers every dashboard
 * write carries (T-530). The token is the one the feed GET minted; fetchJSON
 * swaps in the latest cookie value right before sending.
 */
export function stateRequestInit(
  ids: readonly string[],
  action: "read" | "dismiss",
  confirmToken: string,
): RequestInit {
  return {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Observer-Confirm": confirmToken },
    body: JSON.stringify({ ids, action }),
  };
}

/** badgeText is the count on the bell: "" at zero, "9+" past nine. */
export function badgeText(unread: number): string {
  if (!Number.isFinite(unread) || unread <= 0) return "";
  return unread > 9 ? "9+" : String(Math.floor(unread));
}

/** bellLabel is the bell's accessible name and tooltip. */
export function bellLabel(unread: number): string {
  if (unread <= 0) return "Notifications";
  return `${unread} unread notification${unread === 1 ? "" : "s"}`;
}

/** unreadIds lists the ids to mark read when the bell opens. */
export function unreadIds(rows: readonly NotificationRow[]): string[] {
  return rows.filter((r) => !r.read).map((r) => r.id);
}

/**
 * moneyText formats a row's money figure, or null when there is none or the
 * page renders cost as token usage (the org's developer cost display): the
 * bell never shows a dollar figure the rest of the dashboard hides.
 */
export function moneyText(
  row: Pick<NotificationRow, "usd">,
  dollarsShown: boolean,
  fmtUSD: (n: number) => string,
): string | null {
  if (row.usd == null || !Number.isFinite(row.usd) || !dollarsShown) return null;
  return `${fmtUSD(row.usd)} at risk`;
}

/** severityTone maps a row's severity to the design system's tone. */
export function severityTone(s: NotificationSeverity): "danger" | "warn" | "accent" {
  return s === "critical" ? "danger" : s === "warn" ? "warn" : "accent";
}

/** withoutIds drops rows optimistically after a dismiss. */
export function withoutIds(rows: readonly NotificationRow[], ids: readonly string[]): NotificationRow[] {
  const drop = new Set(ids);
  return rows.filter((r) => !drop.has(r.id));
}
