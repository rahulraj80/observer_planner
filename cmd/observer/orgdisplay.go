package main

import (
	"context"
	"fmt"
	"io"

	"github.com/marmutapp/superbased-observer/internal/orgclient"
)

// reportDisplayPolicy runs the developer cost-display rail once (the same
// verified fetch the daemon's push cycle runs; orgclient.FetchOrgDisplayPolicy)
// and prints what this machine's DASHBOARD will show for cost. It never fails
// the command: a refused or unreachable document leaves the last verified
// value in place, exactly as in the daemon.
//
// The dashboard, the statusline, the reporting CLI (`observer cost`, ...), MCP
// results and the digest follow the org's choice (operator ruling 2026-10-02,
// T-204); this line only reports which choice is in force.
func reportDisplayPolicy(ctx context.Context, out io.Writer, c *orgclient.Client) {
	if c == nil {
		return
	}
	if _, err := c.FetchOrgDisplayPolicy(ctx); err != nil {
		fmt.Fprintf(out, "Dashboard cost display: org policy not refreshed (%v); keeping the last verified value\n", err)
		return
	}
	row, ok, err := c.DisplayPolicy(ctx)
	switch {
	case err != nil || !ok:
		fmt.Fprintln(out, "Dashboard cost display: dollar costs (no org display policy verified yet)")
	case row.DeveloperCostDisplay == "tokens":
		fmt.Fprintf(out, "Dashboard cost display: token usage instead of costs (org policy v%d)\n", row.Version)
	default:
		fmt.Fprintf(out, "Dashboard cost display: dollar costs (org policy v%d)\n", row.Version)
	}
}
