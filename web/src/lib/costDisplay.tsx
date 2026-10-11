import { Fragment, createContext, useContext, useEffect, useState } from "react";
import type { ReactNode } from "react";

import { setCostDisplayMode } from "@shared/lib/format";
import { fetchJSON } from "./api";
import { PENDING_COST_DISPLAY, normalizeCostDisplay, retryDelayMs } from "./costDisplayModel";
import type { CostDisplayInfo, CostDisplayModeName } from "./costDisplayModel";

export { costDisplayNote } from "./costDisplayModel";
export type { CostDisplayInfo } from "./costDisplayModel";

// Developer cost display on the NODE dashboard (operator ruling 2026-10-02):
// "Admins always see cost. Admins can choose whether developers see cost or
// only token usage."
//
// The org admin's choice reaches this node as a signed document the push loop
// verifies and caches; GET /api/cost-display answers from that cache (no
// network call) and says "usd" for a solo install, an unenrolled node, or an
// org that never chose. This file is the ONE place the node dashboard reads
// it, and setCostDisplayMode (shared/lib/format.ts) is the ONE formatter seam
// it feeds: every fmtUSD / fmtCost / shared defaultRenderCost on the page
// follows it, so no surface switches on the mode itself.
//
// BOOT ORDER: the gate resolves the mode BEFORE the app renders, because the
// formatter seam is a plain function (chart tick formatters, table cells,
// strings) that cannot re-render on a context change. The read is a loopback
// call against an in-process cache, so the wait is milliseconds; first paint
// waits at most GATE_TIMEOUT_MS.
//
// FAIL-SAFE (Sol A4-03): until an AUTHORITATIVE answer arrives the dashboard
// renders token usage (PENDING_COST_DISPLAY), never dollars. A stalled, failed
// or unreadable answer used to settle the whole page session on dollars, which
// exposed costs on a node whose org chose token usage (D-013). The gate now
// keeps asking on a bounded backoff; when the answer arrives and differs from
// what is on screen it remounts the app once so every plain formatter re-reads
// the mode. A change the org makes while the dashboard is open applies on the
// next page load.

// GATE_TIMEOUT_MS bounds how long first paint waits for the mode.
const GATE_TIMEOUT_MS = 1500;

// ATTEMPT_TIMEOUT_MS bounds one request, so a hung read is retried.
const ATTEMPT_TIMEOUT_MS = 5000;

const Ctx = createContext<CostDisplayInfo>(PENDING_COST_DISPLAY);

export function CostDisplayGate({ children }: { children: ReactNode }) {
  const [info, setInfo] = useState<CostDisplayInfo | null>(null);
  const [generation, setGeneration] = useState(0);
  useEffect(() => {
    let cancelled = false;
    let resolved = false;
    let rendered: CostDisplayModeName | null = null;
    let retry: number | undefined;
    const show = (next: CostDisplayInfo) => {
      if (cancelled) return;
      setCostDisplayMode(next.mode);
      if (rendered !== null && rendered !== next.mode) setGeneration((g) => g + 1);
      rendered = next.mode;
      setInfo(next);
    };
    const gate = window.setTimeout(() => {
      if (rendered === null) show(PENDING_COST_DISPLAY);
    }, GATE_TIMEOUT_MS);
    const attempt = async (n: number) => {
      const ctl = new AbortController();
      const limit = window.setTimeout(() => ctl.abort(), ATTEMPT_TIMEOUT_MS);
      let next: CostDisplayInfo | null = null;
      try {
        next = normalizeCostDisplay(await fetchJSON<unknown>("/api/cost-display", undefined, { signal: ctl.signal }));
      } catch {
        next = null;
      } finally {
        window.clearTimeout(limit);
      }
      if (cancelled || resolved) return;
      if (next) {
        resolved = true;
        window.clearTimeout(gate);
        show(next);
        return;
      }
      if (rendered === null) {
        window.clearTimeout(gate);
        show(PENDING_COST_DISPLAY);
      }
      retry = window.setTimeout(() => void attempt(n + 1), retryDelayMs(n));
    };
    void attempt(0);
    return () => {
      cancelled = true;
      window.clearTimeout(gate);
      if (retry !== undefined) window.clearTimeout(retry);
    };
  }, []);
  if (!info) return null;
  return (
    <Ctx.Provider value={info}>
      <Fragment key={generation}>{children}</Fragment>
    </Ctx.Provider>
  );
}

export function useCostDisplay(): CostDisplayInfo {
  return useContext(Ctx);
}
