//go:build sbo_omit_egress_pricing_feed

package main

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// pricing_omit.go stands in for pricing.go and pricingautosync.go in a build
// without the egress.pricing-feed component (-tags
// sbo_omit_egress_pricing_feed, lane MP-L1H, T-715): internal/pricingfeed,
// its client and internal/pricingfeedgate are not linked, so this binary
// never fetches the public price table. A feed body a previous build stored
// in pricing_feed_cache is still read by the cost engine (the wire types are
// core, internal/pricingfeedwire); it is simply never refreshed.
// `observer pricing` answers "not in this build" (exit 3).
func init() { components.MarkOmitted("egress.pricing-feed") }

// newPricingCmd registers `observer pricing` as a compiled-out verb.
func newPricingCmd() *cobra.Command {
	return compiledOutVerb("pricing", "Public pricing feed (not in this build)", "egress.pricing-feed")
}

// runPricingAutoSyncFromConfig starts no poller.
func runPricingAutoSyncFromConfig(context.Context, io.Writer, io.Writer, string) {}
