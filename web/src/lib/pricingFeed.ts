// pricingFeed reads the [pricing.feed] block out of a GET /api/config body.
// That endpoint serializes the Go config struct, so the keys are Go field
// names (Pricing.Feed.PollIntervalHours), not the TOML spelling. Returns null
// when the block is absent (an older build), so an absence never reads as a
// verdict.

/** PricingFeed is the node's own pricing-feed egress setting. */
export type PricingFeed = {
  enabled: boolean;
  auto: boolean;
  url: string;
  pollIntervalHours: number;
};

/** pricingFeedFromConfig extracts the feed block, or null when it is absent. */
export function pricingFeedFromConfig(config: Record<string, unknown> | undefined): PricingFeed | null {
  const pricing = config?.Pricing as Record<string, unknown> | undefined;
  const feed = pricing?.Feed as Record<string, unknown> | undefined;
  if (!feed || typeof feed !== "object") return null;
  const hours = typeof feed.PollIntervalHours === "number" && feed.PollIntervalHours > 0 ? feed.PollIntervalHours : 24;
  return {
    enabled: feed.Enabled === true,
    auto: feed.Auto === true,
    url: typeof feed.URL === "string" ? feed.URL : "",
    pollIntervalHours: hours,
  };
}
