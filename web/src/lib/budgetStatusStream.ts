import { useEffect, useState } from "react";
import type { BudgetStatus } from "@/lib/budgetWarning";

// Budget freshness P4 (T-206, D-016): the daemon pushes the org budget state
// over GET /api/budget/status/stream (server-sent events, the same payload as
// GET /api/budget/status) the moment it rewrites budget-status.json. The
// banner keeps its 15 s poll as the fallback, and shows whichever of the two
// was written last (lib/budgetWarning.ts newerBudgetStatus), so a stream that
// dropped never pins an older state.

export const BUDGET_STATUS_STREAM = "/api/budget/status/stream";

/** The latest status the stream sent, or null (none yet, or no EventSource). */
export function useBudgetStatusStream(): BudgetStatus | null {
  const [data, setData] = useState<BudgetStatus | null>(null);
  useEffect(() => {
    if (typeof window === "undefined" || typeof window.EventSource === "undefined") return;
    let source: EventSource;
    try {
      source = new window.EventSource(BUDGET_STATUS_STREAM);
    } catch {
      return;
    }
    const onStatus = (e: Event) => {
      try {
        setData(JSON.parse((e as MessageEvent<string>).data) as BudgetStatus);
      } catch {
        // A malformed frame is ignored; the poll still runs.
      }
    };
    source.addEventListener("status", onStatus);
    return () => {
      source.removeEventListener("status", onStatus);
      source.close();
    };
  }, []);
  return data;
}
