// Package livecheck generates the per-surface live-validation checklist
// (enterprise vendor API plan 12A.4, lane F9) from the catalog: which plan
// and role to obtain, the exact scopes, the host a request goes to, the web2
// steps, the scenarios to observe and how a receipt is recorded. It is pure:
// the caller hands it catalog rows; it reads nothing and calls nothing. The
// committed copy is docs/vendor-connections-live-validation.md, kept equal to
// Render by a drift test, and the Integrations page serves the same text.
package livecheck

import (
	"fmt"
	"sort"
	"strings"

	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// Item is one surface's checklist.
type Item struct {
	Surface  vs.SurfaceID `json:"surface"`
	Vendor   string       `json:"vendor"`
	Plane    string       `json:"plane"`
	AuthKind string       `json:"auth_kind"`
	Scopes   []string     `json:"scopes"`
	Plans    []string     `json:"plans"`
	Requires []string     `json:"requires"`
	TierDoc  string       `json:"tier_doc"`
	Hosts    []string     `json:"hosts"`
	// ResidencyHosts are host templates a connection may expand with its
	// own tenant label (GHE.com); DownloadHosts the second-hop hosts of a
	// two-step report (lane L8). Both empty on most surfaces.
	ResidencyHosts []string `json:"residency_hosts,omitempty"`
	DownloadHosts  []string `json:"download_hosts,omitempty"`
	Supplies       []Supply `json:"supplies"`
	Steps          []string `json:"steps"`
	Scenarios      []string `json:"scenarios"`
	Receipt        []string `json:"receipt"`
	Sources        []string `json:"sources"`
}

// Supply is one supply's current verification state.
type Supply struct {
	Class     string `json:"class"`
	Synthetic string `json:"synthetic"`
	Live      string `json:"live"`
}

// steps are the web2 steps every checklist shares, in order.
var steps = []string{
	"Settings, Integrations: add the vendor account (the tenant id the vendor shows for the organization).",
	"Add an auth binding of the listed kind and paste the credential minted with exactly the listed scopes; Observer seals it and never shows it again.",
	"Add a connection on this surface with the account and binding; leave it disabled.",
	"Test connection: one page is fetched, sanitized and counted, nothing lands. Confirm the resource types and attribute names match the fixture's field ledger.",
	"Enable the connection and use Run now (the runner must be enabled on the server, [vendor_connections].runner_enabled).",
}

// scenarios are the plan 12A.4 scenarios to observe.
var scenarios = []string{
	"a first full run (every page, outcome ok, watermark advanced)",
	"a multi-page run (more than one page requested)",
	"an empty page (the vendor lists nothing for the window)",
	"a 429 with Retry-After, if it can be reproduced without load on the vendor",
	"a revision or correction of an earlier value, where the surface has one",
}

// receipt is how a receipt is recorded (plan 12A.4, 12A.7).
var receipt = []string{
	"Not built yet (a later lane): Record validation receipt (admin:integrations) will select completed real runs, and the server will derive the evidence from them.",
	"Stored: the run ids, path-only response shape digests (field names and types, never values) and the operator's attestation.",
	"A shape digest that differs from the fixture's field ledger opens a fixture drift task and the supply stays live_pending.",
	"Spend approval stays disabled until revision_proven (plan 7.4).",
}

// Build returns the checklist of each surface, sorted by id.
func Build(surfaces []vs.Surface) []Item {
	out := make([]Item, 0, len(surfaces))
	for _, s := range surfaces {
		it := Item{
			Surface: s.ID, Vendor: s.Vendor, Plane: s.Plane, AuthKind: string(s.Auth.Kind),
			Scopes: nonNil(s.Auth.Scopes), Plans: nonNil(s.Tier.Plans), Requires: nonNil(s.Tier.Requires), TierDoc: s.Tier.Doc,
			Steps: steps, Scenarios: scenarios, Receipt: receipt,
		}
		for _, h := range s.Hosts {
			it.Hosts = append(it.Hosts, fmt.Sprintf("%s://%s:%d", h.Scheme, h.Host, h.Port))
		}
		for _, h := range s.HostTemplates {
			it.ResidencyHosts = append(it.ResidencyHosts, h.String())
		}
		for _, h := range s.DownloadHosts {
			it.DownloadHosts = append(it.DownloadHosts, h.String())
		}
		for _, h := range s.DownloadHostTemplates {
			it.DownloadHosts = append(it.DownloadHosts, h.String())
		}
		for _, sup := range s.Supplies {
			it.Supplies = append(it.Supplies, Supply{Class: string(sup.Class), Synthetic: string(sup.Verification.Synthetic), Live: string(sup.Verification.Live)})
		}
		for _, src := range s.Sources {
			if src.URL != "" {
				it.Sources = append(it.Sources, src.URL+" (checked "+src.CheckedOn+")")
			}
		}
		it.Hosts, it.Supplies, it.Sources = nonNil(it.Hosts), nonNilS(it.Supplies), nonNil(it.Sources)
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Surface < out[j].Surface })
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilS(s []Supply) []Supply {
	if s == nil {
		return []Supply{}
	}
	return s
}

func listOr(xs []string, none string) string {
	if len(xs) == 0 {
		return none
	}
	return strings.Join(xs, ", ")
}

// Render is the markdown of the committed checklist document.
func Render(items []Item) string {
	var b strings.Builder
	b.WriteString("# Vendor connections: live validation checklists\n\n")
	b.WriteString("GENERATED by internal/vendorsurface/livecheck from the catalog and the driver table (enterprise\n")
	b.WriteString("vendor API plan 12A.4, lane F9). Do not edit by hand: change the catalog row, then run\n")
	b.WriteString("`go test ./internal/vendorsurface/livecheck/ -run TestChecklistDocIsCurrent -update`.\n\n")
	b.WriteString("No surface is live-validated. Every surface below is synthetic only: no vendor response has been\n")
	b.WriteString("observed. Live validation is operator-attended, needs real keys, and never blocks a lane.\n")
	for _, it := range items {
		fmt.Fprintf(&b, "\n## %s\n\n", it.Surface)
		fmt.Fprintf(&b, "- Vendor and plane: %s, %s\n", it.Vendor, it.Plane)
		fmt.Fprintf(&b, "- Plan and role to obtain: %s; requires %s (catalog: %q)\n", listOr(it.Plans, "not stated in the catalog"), listOr(it.Requires, "nothing further stated"), it.TierDoc)
		fmt.Fprintf(&b, "- Credential: auth kind `%s`, scopes %s\n", it.AuthKind, listOr(it.Scopes, "not stated in the catalog (mint the narrowest read-only credential the vendor offers)"))
		fmt.Fprintf(&b, "- Requests go to: %s\n", listOr(it.Hosts, "no host tuple declared"))
		if len(it.ResidencyHosts) > 0 {
			fmt.Fprintf(&b, "- Data residency hosts (the connection names its own tenant label): %s\n", listOr(it.ResidencyHosts, ""))
		}
		if len(it.DownloadHosts) > 0 {
			fmt.Fprintf(&b, "- Report downloads (signed links, no credential sent) go to: %s\n", listOr(it.DownloadHosts, ""))
		}
		for _, s := range it.Supplies {
			fmt.Fprintf(&b, "- Supply %s: synthetic `%s`, live `%s`\n", s.Class, s.Synthetic, s.Live)
		}
		b.WriteString("\nSteps in web2:\n\n")
		for i, s := range it.Steps {
			fmt.Fprintf(&b, "%d. %s\n", i+1, s)
		}
		b.WriteString("\nScenarios to observe:\n\n")
		for _, s := range it.Scenarios {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\nRecording a receipt:\n\n")
		for _, s := range it.Receipt {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		if len(it.Sources) > 0 {
			b.WriteString("\nSources:\n\n")
			for _, s := range it.Sources {
				fmt.Fprintf(&b, "- %s\n", s)
			}
		}
	}
	return b.String()
}
