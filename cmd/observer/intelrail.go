package main

import (
	"context"
	"fmt"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/govern"
	"github.com/marmutapp/superbased-observer/internal/orgintel"
)

// intelrail.go explains, on a MANAGED machine, why org session enrichment does
// or does not run here (operator ruling Q2, 2026-10-04). The answer is the
// same gate the result rail and the background sweep use (intelRailEnabled
// over the live governance posture); this file only names the reason in plain
// words, so the Settings card and `observer intel status` say the same thing.

// orgIntelRailRule is one row of the readout table: the first row whose match
// holds names the reason. Walked top-down (CLAUDE.md #5).
type orgIntelRailRule struct {
	reason  string
	on      bool
	match   func(eff govern.Effective, local bool) bool
	message func(org string) string
}

var orgIntelRailRules = []orgIntelRailRule{
	{
		reason: "on",
		on:     true,
		match:  func(eff govern.Effective, local bool) bool { return intelRailEnabled(local, eff) },
		message: func(org string) string {
			return fmt.Sprintf("Org enrichment runs on this machine under %s's terms.", org)
		},
	},
	{
		reason: "no_policy",
		match:  func(eff govern.Effective, _ bool) bool { return eff.State == govern.StateNoPolicy },
		message: func(org string) string {
			return fmt.Sprintf("Org enrichment is not running on this machine yet: %s has not published a machine policy for it. Nothing is sent until an admin turns it on for managed machines.", org)
		},
	},
	{
		reason: "policy_not_in_force",
		match:  func(eff govern.Effective, _ bool) bool { return eff.State != govern.StateApplied },
		message: func(org string) string {
			return fmt.Sprintf("Org enrichment is not running on this machine: %s's machine policy is not in force here right now (see Settings, Enrolment). Nothing is sent.", org)
		},
	},
	{
		reason: "no_authority",
		match:  func(eff govern.Effective, _ bool) bool { return !eff.GrantsIntelExtraction() },
		message: func(org string) string {
			return fmt.Sprintf("Org enrichment is not running on this machine: its enrolment with %s does not include session enrichment. An admin can re-issue the enrolment with it included. Nothing is sent.", org)
		},
	},
	{
		reason: "not_raised",
		match:  func(govern.Effective, bool) bool { return true },
		message: func(org string) string {
			return fmt.Sprintf("Org enrichment is not running on this machine: %s has not turned it on for managed machines. Nothing is sent.", org)
		},
	},
}

// orgIntelManagedRailFor walks orgIntelRailRules. Pure: the table test drives
// it with hand-built postures.
func orgIntelManagedRailFor(eff govern.Effective, local bool, org string) dashapi.OrgIntelRailView {
	for _, r := range orgIntelRailRules {
		if r.match(eff, local) {
			return dashapi.OrgIntelRailView{On: r.on, Reason: r.reason, Message: r.message(org)}
		}
	}
	last := orgIntelRailRules[len(orgIntelRailRules)-1]
	return dashapi.OrgIntelRailView{Reason: last.reason, Message: last.message(org)}
}

// orgIntelGovernance resolves the live governance posture the way the daemon
// does (the CLI and the dashboard both read it through here).
func orgIntelGovernance(ctx context.Context, b orgBundle) govern.Effective {
	return enterpriseContentEffective(ctx, b.cfg, b.store, nil)
}

// setOrgIntelManagedRail fills v.ManagedRail on a managed machine; any other
// class leaves it nil.
func setOrgIntelManagedRail(v *dashapi.OrgIntelStatus, snap orgIntelSnapshot, eff govern.Effective) {
	if snap.Status.Class != orgintel.ClassManaged {
		return
	}
	rail := orgIntelManagedRailFor(eff, snap.ConfigOptIn, orgIntelSnapshotOrgName(snap))
	v.ManagedRail = &rail
}

// orgIntelSnapshotOrgName names the organization for a sentence.
func orgIntelSnapshotOrgName(snap orgIntelSnapshot) string {
	if snap.Enrolment != nil {
		if snap.Enrolment.OrgName != "" {
			return snap.Enrolment.OrgName
		}
		if snap.Enrolment.OrgID != "" {
			return snap.Enrolment.OrgID
		}
	}
	return "Your organization"
}
