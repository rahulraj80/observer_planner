package main

import (
	"net/http"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/orgclient"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// ProjectDigests serves GET /api/intel/digests (T-490): a signed pull of the
// weekly org digests of the projects this member works in. The org server
// scopes, audits and filters it; this side only relays the outcome.
func (d *orgIntelDashboard) ProjectDigests(r *http.Request) (dashapi.OrgIntelDigests, error) {
	b, err := d.build(r.Context(), d.configPath)
	if err != nil {
		return dashapi.OrgIntelDigests{}, err
	}
	defer b.cleanup()
	out := dashapi.OrgIntelDigests{}
	if enr, eerr := b.store.LoadEnrolment(r.Context()); eerr == nil && enr != nil {
		out.OrgName = enr.OrgName
	}
	// Every failure is already a state the page renders (not_enrolled,
	// unreachable, malformed, ...); the error text is log-only and this read
	// has no logger of its own, so the state alone is returned.
	f, _ := b.client.FetchProjectDigests(r.Context()) //nolint:errcheck // the outcome state carries it
	return newOrgIntelDigestsView(out, f), nil
}

// newOrgIntelDigestsView maps the rail outcome onto the dashboard body.
func newOrgIntelDigestsView(out dashapi.OrgIntelDigests, f orgclient.DigestFetch) dashapi.OrgIntelDigests {
	out.State = f.State
	out.Projects = f.Projects
	out.Withheld = f.Withheld
	out.Digests = make([]dashapi.OrgIntelDigestView, 0, len(f.Digests))
	for _, row := range f.Digests {
		out.Digests = append(out.Digests, digestView(row))
	}
	return out
}

func digestView(r orgcontract.IntelDigestRow) dashapi.OrgIntelDigestView {
	return dashapi.OrgIntelDigestView{
		ProjectID:             r.ProjectID,
		ProjectLabel:          r.ProjectLabel,
		WeekStart:             r.WeekStart,
		PeriodEnd:             r.PeriodEnd,
		SessionCount:          r.SessionCount,
		Headline:              r.Headline,
		Themes:                r.Themes,
		RecurringErrorClasses: r.RecurringErrorClasses,
		UnfinishedThreads:     r.UnfinishedThreads,
		SuggestedNextSession:  r.SuggestedNextSession,
		UpdatedAt:             r.UpdatedAt,
	}
}

// The dashboard asserts the optional digest seam on Options.OrgIntel.
var _ dashapi.OrgIntelDigestReader = (*orgIntelDashboard)(nil)
