import type { ReactNode } from "react";
import { Tooltip } from "../../primitives";
import { fmtCost, fmtUSD, getCostDisplayMode } from "../../lib/format";

// RenderCostContext is the optional second argument every internal cost call
// passes: the token count behind this figure (when known) and the KIND of
// figure it is. The org's <Money> renderer reads `tokens` to keep its
// token/percent-of-cap link in tokens display mode, and may read `kind` to
// style an api / tool / total / model / kpi figure differently. The node's
// defaultRenderCost ignores it entirely (bare-USD, unchanged).
export type RenderCostContext = {
  tokens?: number;
  kind: "api" | "tool" | "total" | "model" | "kpi";
};

// RenderCost renders one USD figure as a display node. It is the seam every
// promoted session-detail cost cell takes so the org can render tokens /
// percent-of-cap through its <Money> formatter instead of bare USD, while the
// node keeps its fmtUSD display (the default below). The optional `ctx` carries
// the token count + figure kind; a renderer that only wants the dollar amount
// (like defaultRenderCost) simply ignores the second parameter.
export type RenderCost = (usd: number, ctx?: RenderCostContext) => ReactNode;

// defaultRenderCost is the node's behaviour: the compact dollar figure with the
// exact value on hover. It is the default for every renderCost slot, so a
// caller that passes nothing gets the node's rendering unchanged. It ignores
// the RenderCostContext (a one-parameter function is assignable to RenderCost).
//
// When the developer's org chose token usage for developers' dashboards
// (format.ts "Developer cost display") it renders the token count the caller
// passed in ctx, or the hidden-value glyph where there is none; the precise-
// dollar tooltip is dropped because it would print the figure the org asked
// not to show. The org passes its own renderer, so web2 is unaffected.
export const defaultRenderCost: RenderCost = (usd, ctx) =>
  getCostDisplayMode() === "tokens" ? (
    <span>{fmtCost(usd, ctx?.tokens)}</span>
  ) : (
    <Tooltip content={fmtUSD(usd, true)}>
      <span tabIndex={0} className="cursor-help focus:outline-none">
        {fmtUSD(usd)}
      </span>
    </Tooltip>
  );
