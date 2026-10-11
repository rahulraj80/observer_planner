package vendorsanitize

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/scrub"
	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

// activityCanary is a credential-shaped value assembled at run time so no
// credential-shaped literal sits in the source.
var activityCanary = "gh" + "p_" + strings.Repeat("Z9", 18)

func auditPage() vendorapi.Page {
	return vendorapi.Page{Audit: []vendorapi.AuditEvent{{
		Namespace: "cursor.audit", EventID: "e1", At: time.Unix(1, 0), Action: "login", ActorKind: "email",
		ActorIdentity: "dev@example.test",
		Detail: map[string]string{
			"success": "true", "role": activityCanary, "prompt_text": "drop me",
			"source": strings.Repeat("word ", 30),
		},
	}}}
}

// TestActivityAllowlistsFailClosed (lane F11): an audit namespace, usage join
// namespace or snapshot class with no reviewed row refuses the page; a
// detail key off the allowlist is dropped; a typed secret in an allowed
// field is redacted; prose is dropped; secret material in an identity
// refuses the page.
func TestActivityAllowlistsFailClosed(t *testing.T) {
	out, rep, err := Page(auditPage())
	if err != nil {
		t.Fatal(err)
	}
	d := out.Audit[0].Detail
	if d["success"] != "true" || d["role"] != scrub.Redacted {
		t.Errorf("detail %v: want success kept and the canary redacted", d)
	}
	if _, ok := d["prompt_text"]; ok {
		t.Error("an unlisted detail key survived")
	}
	if _, ok := d["source"]; ok {
		t.Error("prose survived in a non-content field")
	}
	if rep.Redacted != 1 || rep.DroppedFields != 1 || rep.DroppedValues != 1 {
		t.Errorf("report %+v", rep)
	}
	for _, c := range []struct {
		name string
		p    vendorapi.Page
	}{
		{"unknown audit namespace", func() vendorapi.Page { p := auditPage(); p.Audit[0].Namespace = "other.audit"; return p }()},
		{"usage namespace without a row", vendorapi.Page{Usage: []vendorapi.UsageRow{{JoinNamespace: "cursor.usage_event", JoinValue: "u1"}}}},
		{"snapshot class without a row", vendorapi.Page{Snapshots: []vendorapi.VendorSnapshot{{Class: "limits"}}}},
	} {
		if _, _, err := Page(c.p); err == nil || !errors.Is(err, ErrRefused) {
			t.Errorf("%s: err %v, want a refusal", c.name, err)
		}
	}
	// Secret material (or an over-long value) in a row's own key quarantines
	// that row; the page lands (VCONN-5, T-695).
	for _, c := range []struct {
		name string
		p    vendorapi.Page
	}{
		{"secret in an audit identity", func() vendorapi.Page { p := auditPage(); p.Audit[0].ActorIdentity = activityCanary; return p }()},
		{"over-long audit identity", func() vendorapi.Page {
			p := auditPage()
			p.Audit[0].ActorIdentity = strings.Repeat("a", vendorapi.MaxIDLen+1)
			return p
		}()},
		{"secret in a fact dimension", vendorapi.Page{Facts: []vendorapi.FactRow{{RepoHash: activityCanary}}}},
		{"secret in a usage key", vendorapi.Page{Usage: []vendorapi.UsageRow{{JoinNamespace: "cursor.usage", JoinValue: activityCanary}}}},
		{"secret in a snapshot subject", vendorapi.Page{Snapshots: []vendorapi.VendorSnapshot{{Class: "cursor.member_spend", Subject: activityCanary}}}},
		{"secret in a retraction key", vendorapi.Page{Retractions: []vendorapi.Retraction{{JoinValue: activityCanary}}}},
	} {
		out, rep, err := Page(c.p)
		if err != nil || rep.Quarantined != 1 || out.HasActivity() {
			t.Errorf("%s: err %v report %+v activity %v, want the row quarantined", c.name, err, rep, out.HasActivity())
		}
	}
	if !HasAuditAllowlist("cursor.audit") || HasAuditAllowlist("nope") {
		t.Error("HasAuditAllowlist")
	}
	// The canary never survives anywhere in the cleaned page.
	if strings.Contains(out.Audit[0].Target+out.Audit[0].ActorIdentity, activityCanary) {
		t.Error("canary survived")
	}
}
