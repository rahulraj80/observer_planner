//go:build sbo_omit_egress_aggregate_share

package main

import (
	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// aggregate_omit.go stands in for aggregate.go in a build without the
// egress.aggregate-share component (-tags sbo_omit_egress_aggregate_share,
// lane MP-L1E, T-715): the aggregate rail (internal/aggregate, its client,
// source and service) and the store's consent / submission seam
// (internal/store/aggregate.go) are not linked, so nothing can submit.
// `observer aggregate` answers "not in this build" (exit 3).
func init() { components.MarkOmitted("egress.aggregate-share") }

// newAggregateCmd registers `observer aggregate` as a compiled-out verb.
func newAggregateCmd() *cobra.Command {
	return compiledOutVerb("aggregate", "Opt-in anonymous aggregate submission (not in this build)", "egress.aggregate-share")
}
