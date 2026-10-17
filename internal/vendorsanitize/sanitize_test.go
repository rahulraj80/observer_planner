package vendorsanitize

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/scrub"
	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// Canary secrets are assembled at run time so no credential-shaped literal
// sits in the source.
var (
	canaryPAT    = "gh" + "p_" + strings.Repeat("Q7", 18)
	canaryBearer = "Bear" + "er " + "sk-" + "live-" + strings.Repeat("Z9", 12)
)

func seat(attrs map[string]string) vendorapi.ResourceObservation {
	parent := vendorapi.NodeRef{Kind: vs.KindOrg, ID: vs.ExternalID{Namespace: "github.org", Value: "acme"}}
	return vendorapi.ResourceObservation{
		Type: "Seat/github.copilot_seat/v1", ID: vs.ExternalID{Namespace: "github.copilot_seat", Value: "acme:N1"},
		Parent: &parent, DisplayName: "octocat", Attrs: attrs,
	}
}

func page(rs ...vendorapi.ResourceObservation) vendorapi.Page {
	return vendorapi.Page{Resources: rs, Snapshot: &vendorapi.SnapshotManifest{ManifestID: "m"}}
}

// TestSanitizerRejectsContentInNonContentFields: one case per value rule.
func TestSanitizerRejectsContentInNonContentFields(t *testing.T) {
	prose := strings.Repeat("word ", maxWords+1)
	cases := []struct {
		name     string
		attrs    map[string]string
		wantKeep map[string]string
		want     Report
	}{
		{"metadata kept", map[string]string{"plan_type": "business"}, map[string]string{"plan_type": "business"}, Report{}},
		{
			"unlisted field dropped",
			map[string]string{"plan_type": "business", "email": "a@b.example"},
			map[string]string{"plan_type": "business"},
			Report{DroppedFields: 1},
		},
		{"line break dropped", map[string]string{"plan_type": "a\nb"}, map[string]string{}, Report{DroppedValues: 1}},
		{"control character dropped", map[string]string{"plan_type": "a\x00b"}, map[string]string{}, Report{DroppedValues: 1}},
		{"oversize dropped", map[string]string{"plan_type": strings.Repeat("x", vendorapi.MaxAttrValue+1)}, map[string]string{}, Report{DroppedValues: 1}},
		{"prose dropped", map[string]string{"last_activity_editor": prose}, map[string]string{}, Report{DroppedValues: 1}},
		{
			"secret redacted",
			map[string]string{"last_activity_editor": "vscode " + canaryPAT},
			map[string]string{"last_activity_editor": scrub.Redacted},
			Report{Redacted: 1},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, rep, err := Page(page(seat(c.attrs)))
			if err != nil {
				t.Fatal(err)
			}
			got := out.Resources[0].Attrs
			if len(got) != len(c.wantKeep) {
				t.Fatalf("attrs = %v, want %v", got, c.wantKeep)
			}
			for k, v := range c.wantKeep {
				if got[k] != v {
					t.Fatalf("attr %s = %q, want %q", k, got[k], v)
				}
			}
			if rep != c.want {
				t.Fatalf("report = %+v, want %+v", rep, c.want)
			}
		})
	}
}

func TestSanitizerRefusesPages(t *testing.T) {
	unknown := seat(nil)
	unknown.Type = "Seat/github.something_new/v1"
	secretID := seat(nil)
	secretID.ID.Value = canaryPAT
	secretParent := seat(nil)
	secretParent.Parent = &vendorapi.NodeRef{Kind: vs.KindOrg, ID: vs.ExternalID{Namespace: "github.org", Value: canaryPAT}}
	edge := page(seat(nil))
	edge.Edges = []vendorapi.EdgeObservation{{
		Edge: vs.EdgeLicensedTo, From: vendorapi.NodeRef{Kind: vs.KindSeat, ID: secretID.ID},
		To: vendorapi.NodeRef{Kind: vs.KindUser, ID: vs.ExternalID{Namespace: "github.user", Value: "N1"}},
	}}
	scopeParent := page(seat(nil))
	scopeParent.Snapshot.ScopeParent = &vendorapi.NodeRef{Kind: vs.KindOrg, ID: vs.ExternalID{Namespace: "github.org", Value: canaryPAT}}
	for name, p := range map[string]vendorapi.Page{
		"type with no allowlist row":        page(unknown),
		"secret in the manifest scope root": scopeParent,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := Page(p)
			if !errors.Is(err, ErrRefused) {
				t.Fatalf("want refusal, got %v", err)
			}
			if strings.Contains(err.Error(), canaryPAT) {
				t.Fatalf("refusal names the secret: %v", err)
			}
		})
	}
	// A secret in a ROW's own key quarantines that row (and an edge naming
	// it) instead of refusing the page (VCONN-5, T-695): the other rows land
	// and the page is never authoritative for deletion.
	good := seat(nil)
	good.ID.Value = "acme:N2"
	for name, c := range map[string]struct {
		p        vendorapi.Page
		resLeft  int
		edgeLeft int
		quar     int
	}{
		"secret in an id":            {page(secretID, good), 1, 0, 1},
		"secret in a parent":         {page(secretParent, good), 1, 0, 1},
		"secret in an edge endpoint": {edge, 1, 0, 1},
		"edge to a quarantined node": {func() vendorapi.Page {
			p := page(secretID, good)
			p.Edges = []vendorapi.EdgeObservation{{
				Edge: vs.EdgeLicensedTo, From: vendorapi.NodeRef{Kind: vs.KindSeat, ID: good.ID},
				To: vendorapi.NodeRef{Kind: vs.KindSeat, ID: secretID.ID},
			}}
			return p
		}(), 1, 0, 2},
	} {
		t.Run("quarantine "+name, func(t *testing.T) {
			c.p.Snapshot.Complete, c.p.Snapshot.AuthoritativeForDeletion = true, true
			out, rep, err := Page(c.p)
			if err != nil {
				t.Fatalf("page refused: %v", err)
			}
			if len(out.Resources) != c.resLeft || len(out.Edges) != c.edgeLeft || rep.Quarantined != c.quar || rep.QuarantinedGraph != c.quar {
				t.Fatalf("resources %d edges %d report %+v, want %d, %d and %d quarantined", len(out.Resources), len(out.Edges),
					rep, c.resLeft, c.edgeLeft, c.quar)
			}
			if out.Snapshot.AuthoritativeForDeletion {
				t.Fatal("a page with a quarantined resource stayed authoritative for deletion")
			}
			b, _ := json.Marshal(out)
			if strings.Contains(string(b), canaryPAT) {
				t.Fatalf("canary survived: %s", b)
			}
		})
	}
}

// TestOneVendorValueNeverStallsTheSurface is the VCONN-5 pin (T-695): every
// value the sanitizer lets through also passes vendorapi.Validate, so a page
// with one 300-character display name, one 300-character identity and one
// secret-shaped key name lands every other row instead of being refused on
// every run (the watermark used to stop for good).
func TestOneVendorValueNeverStallsTheSurface(t *testing.T) {
	long := strings.Repeat("n", 300)
	named := seat(nil)
	named.DisplayName = long
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	fact := func(identity string) vendorapi.FactRow {
		return vendorapi.FactRow{
			IntervalStart: day, IntervalEnd: day.AddDate(0, 0, 1), Metric: "sessions", Unit: "count",
			IdentityKind: "api_key_name", Identity: identity,
		}
	}
	p := page(named)
	p.Snapshot.Scope = []vendorapi.ScopeKey{{Namespace: named.ID.Namespace, Kind: vs.KindSeat}}
	p.Snapshot.ObservedAt = day
	p.Facts = []vendorapi.FactRow{fact("ci-runner"), fact("sk-ant-" + canaryPAT), fact(long)}
	p.Usage = []vendorapi.UsageRow{{
		JoinKind: "event", JoinNamespace: "cursor.usage", JoinValue: "u1", At: day, IdentityKind: "email",
		Identity: "dev@example.test", Billing: &vendorapi.UsageBilling{Category: long},
	}}
	out, rep, err := Page(p)
	if err != nil {
		t.Fatalf("page refused: %v", err)
	}
	if err := vendorapi.Validate(out); err != nil {
		t.Fatalf("the sanitized page still fails Validate: %v", err)
	}
	if out.Resources[0].DisplayName != "" || len(out.Facts) != 1 || out.Facts[0].Identity != "ci-runner" ||
		len(out.Usage) != 1 || out.Usage[0].Billing.Category != "" {
		t.Fatalf("cleaned page %+v", out)
	}
	if rep.Quarantined != 2 || rep.QuarantinedGraph != 0 || rep.DroppedValues != 2 {
		t.Fatalf("report %+v, want 2 quarantined facts and 2 dropped names", rep)
	}
}

// TestCanarySecretsNeverSurviveTheBoundary plants canary secrets everywhere a
// value can sit (allowed fields, unlisted secret-named fields, the display
// name, edge attributes) and checks the serialized output carries none.
func TestCanarySecretsNeverSurviveTheBoundary(t *testing.T) {
	r := seat(map[string]string{
		"last_activity_editor": canaryBearer, "plan_type": "business " + canaryPAT,
		"token": canaryPAT, "api_key": canaryPAT, "nested_json": `{"authorization":"` + canaryBearer + `"}`,
	})
	r.DisplayName = canaryPAT
	p := page(r)
	p.Edges = []vendorapi.EdgeObservation{{
		Edge: vs.EdgeLicensedTo, From: vendorapi.NodeRef{Kind: vs.KindSeat, ID: r.ID},
		To:    vendorapi.NodeRef{Kind: vs.KindUser, ID: vs.ExternalID{Namespace: "github.user", Value: "N1"}},
		Attrs: map[string]string{"secret": canaryPAT},
	}}
	out, rep, err := Page(p)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	for _, c := range []string{canaryPAT, "sk-live-"} {
		if strings.Contains(string(b), c) {
			t.Fatalf("canary %q survived: %s", c, b)
		}
	}
	if rep.Redacted == 0 || rep.DroppedFields == 0 {
		t.Fatalf("report %+v does not show the redaction and the drops", rep)
	}
	if !HasAllowlist("Seat/github.copilot_seat/v1") || HasAllowlist("Credential/github.pat/v1") {
		t.Fatal("allowlist rows")
	}
}

func TestSanitizerDoesNotModifyInput(t *testing.T) {
	in := page(seat(map[string]string{"email": "x"}))
	if _, _, err := Page(in); err != nil {
		t.Fatal(err)
	}
	if in.Resources[0].Attrs["email"] != "x" {
		t.Fatal("input page modified")
	}
}
