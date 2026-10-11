import { dollarsShown, maskDollarAmounts } from "../../../shared/lib/format.ts";

// costExport - the ONE seam every node-dashboard export (a page's CSV
// download, the TopBar's JSON Export) goes through so a download follows the
// developer cost display exactly as the page does (T-204, D-013).
//
// Choice: an export FOLLOWS the display mode and its units are in the column
// names. In dollars it is unchanged (the `_usd` columns say so). In the
// token-usage view the dollar columns are left out and the token columns
// (`_tokens`) stay; a JSON export also drops its dollar fields, hides dollar
// amounts in its text and gains "cost_display": "tokens", so a reader can
// tell the omission is deliberate. Nothing is ever converted from dollars
// into tokens.
//
// isDollarKey is the twin of internal/costtext.IsDollarKey (Go); the two are
// pinned by the same table of keys (costExport.test.ts, costtext_test.go).
// Pure: no React, no DOM, so it runs under node --test.

/** isDollarKey reports whether a field or column name carries a dollar amount. */
export function isDollarKey(key: string): boolean {
  if (key.endsWith("USD") || key.endsWith("Usd")) return true;
  const k = key.toLowerCase();
  if (k === "usd" || k === "cost") return true;
  return k.split(/[_.-]/).includes("usd");
}

/** CsvColumn is one export column: its header (with its unit) and its cell. */
export interface CsvColumn<T> {
  header: string;
  value: (row: T) => string | number | null | undefined;
}

/** csvEscape quotes a cell when CSV needs it. */
export function csvEscape(v: string | number | null | undefined): string {
  if (v == null) return "";
  const s = String(v);
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

/**
 * exportColumns is the column set an export writes under the current cost
 * display: every column in dollars; every non-dollar column in the
 * token-usage view.
 */
export function exportColumns<T>(columns: readonly CsvColumn<T>[]): CsvColumn<T>[] {
  return dollarsShown() ? [...columns] : columns.filter((c) => !isDollarKey(c.header));
}

/** csvText renders rows through the display-aware column set. */
export function csvText<T>(rows: readonly T[], columns: readonly CsvColumn<T>[]): string {
  const cols = exportColumns(columns);
  const lines = rows.map((r) => cols.map((c) => csvEscape(c.value(r))).join(","));
  return [cols.map((c) => c.header).join(","), ...lines].join("\n");
}

function stripDollars(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(stripDollars);
  if (typeof v === "string") return maskDollarAmounts(v);
  if (v && typeof v === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, val] of Object.entries(v as Record<string, unknown>)) {
      if (!isDollarKey(k)) out[k] = stripDollars(val);
    }
    return out;
  }
  return v;
}

/**
 * exportPayload shapes a JSON export for the current cost display: the
 * identity in dollars; in the token-usage view dollar fields dropped, dollar
 * amounts in text hidden, and a top-level object marked cost_display.
 */
export function exportPayload(value: unknown): unknown {
  if (dollarsShown()) return value;
  const shaped = stripDollars(value);
  if (shaped && typeof shaped === "object" && !Array.isArray(shaped)) {
    return { ...(shaped as Record<string, unknown>), cost_display: "tokens" };
  }
  return shaped;
}

/**
 * exportJSONText is the TopBar Export body: the endpoint's JSON text, shaped
 * for the display. Text that is not JSON is returned as it came.
 */
export function exportJSONText(text: string): string {
  if (dollarsShown()) return text;
  try {
    return JSON.stringify(exportPayload(JSON.parse(text)), null, 2);
  } catch {
    return maskDollarAmounts(text);
  }
}
