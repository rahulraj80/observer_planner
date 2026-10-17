package vendorapi

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var at0 = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func okActivity() Page {
	return Page{
		Audit: []AuditEvent{{
			Namespace: "cursor.audit", EventID: "e1", At: at0, ActorKind: "email", ActorIdentity: "a@x.test",
			Action: "login", Detail: map[string]string{"success": "true"},
		}},
		Usage: []UsageRow{{
			JoinKind: "usage_event_id", JoinNamespace: "cursor.usage_event", JoinValue: "u1", At: at0,
			IdentityKind: "email", Identity: "a@x.test", ChargedUnit: "cents", ChargedMicros: 5,
		}},
		Facts:       []FactRow{{IntervalStart: at0, IntervalEnd: at0.Add(24 * time.Hour), Metric: "accepted_lines", Unit: "count", ValueMicros: 1}},
		Snapshots:   []VendorSnapshot{{Class: "limits", Subject: "org", Body: map[string]string{"cap": "10"}}},
		Retractions: []Retraction{{JoinKind: "usage_event_id", JoinNamespace: "cursor.usage_event", JoinValue: "u0"}},
	}
}

// TestValidateActivityTable: one case per row of every activity rule table
// (lane F11), plus the in-page repeat refusal.
func TestValidateActivityTable(t *testing.T) {
	if err := ValidateActivity(okActivity()); err != nil {
		t.Fatalf("base page refused: %v", err)
	}
	cases := []struct {
		name string
		mut  func(*Page)
		want string
	}{
		{"audit namespace", func(p *Page) { p.Audit[0].Namespace = "Cursor Audit" }, "audit namespace"},
		{"audit id", func(p *Page) { p.Audit[0].EventID = "" }, "audit event id"},
		{"audit time", func(p *Page) { p.Audit[0].At = time.Time{} }, "audit event time"},
		{"audit action", func(p *Page) { p.Audit[0].Action = "Login Event" }, "audit action"},
		{"audit actor", func(p *Page) { p.Audit[0].ActorKind = "" }, "audit actor"},
		{"audit target", func(p *Page) { p.Audit[0].Target = strings.Repeat("x", MaxTargetLen+1) }, "audit target"},
		{"audit detail limits", func(p *Page) { p.Audit[0].Detail = map[string]string{"Bad Key": "v"} }, "audit detail within"},
		{"audit secret key", func(p *Page) { p.Audit[0].Detail = map[string]string{"api_key": "v"} }, "secret-shaped"},
		{"usage join kind", func(p *Page) { p.Usage[0].JoinKind = "" }, "usage join kind"},
		{"usage join namespace", func(p *Page) { p.Usage[0].JoinNamespace = "X" }, "usage join namespace"},
		{"usage join value", func(p *Page) { p.Usage[0].JoinValue = strings.Repeat("v", MaxIDLen+1) }, "usage join value"},
		{"usage time", func(p *Page) { p.Usage[0].At = time.Time{} }, "usage time"},
		{"usage identity", func(p *Page) { p.Usage[0].Identity = "" }, "usage identity"},
		{"usage negative", func(p *Page) { p.Usage[0].InputTokens = -1 }, "not negative"},
		{"usage unit", func(p *Page) { p.Usage[0].ChargedUnit = "" }, "names its unit"},
		{"usage model", func(p *Page) { p.Usage[0].Model = strings.Repeat("m", MaxIDLen+1) }, "model and session"},
		{"usage flags", func(p *Page) { p.Usage[0].Flags = map[string]string{"token": "v"} }, "usage flags"},
		{"usage billing amount", func(p *Page) { neg := int64(-1); p.Usage[0].Billing = &UsageBilling{FeeMicros: &neg} }, "billing amounts"},
		{"usage billing name", func(p *Page) { p.Usage[0].Billing = &UsageBilling{AutomationID: strings.Repeat("a", MaxIDLen+1)} }, "billing names"},
		{"fact interval", func(p *Page) { p.Facts[0].IntervalEnd = at0 }, "fact interval"},
		{"fact metric", func(p *Page) { p.Facts[0].Metric = "" }, "fact metric"},
		{"fact unit", func(p *Page) { p.Facts[0].Unit = "US Dollars" }, "fact unit"},
		{"fact identity", func(p *Page) { p.Facts[0].IdentityKind = "email" }, "fact identity"},
		{"fact dimension", func(p *Page) { p.Facts[0].Feature = strings.Repeat("f", MaxIDLen+1) }, "fact dimensions"},
		{"fact revision", func(p *Page) { p.Facts[0].Revision = -1 }, "fact revision"},
		{"snapshot class", func(p *Page) { p.Snapshots[0].Class = "" }, "snapshot class"},
		{"snapshot subject", func(p *Page) { p.Snapshots[0].Subject = strings.Repeat("s", MaxIDLen+1) }, "snapshot subject"},
		{"snapshot body", func(p *Page) { p.Snapshots[0].Body = map[string]string{"BAD": "v"} }, "snapshot body within"},
		{"snapshot secret", func(p *Page) { p.Snapshots[0].Body = map[string]string{"password": "v"} }, "snapshot body carries"},
		{"retraction kind", func(p *Page) { p.Retractions[0].JoinKind = "" }, "retraction join kind"},
		{"retraction namespace", func(p *Page) { p.Retractions[0].JoinNamespace = "" }, "retraction join namespace"},
		{"retraction value", func(p *Page) { p.Retractions[0].JoinValue = "" }, "retraction join value"},
		{"retraction reason", func(p *Page) { p.Retractions[0].Reason = "Because I said so" }, "retraction reason"},
		{"repeat", func(p *Page) { p.Audit = append(p.Audit, p.Audit[0]) }, "repeats its key"},
	}
	for _, c := range cases {
		p := okActivity()
		c.mut(&p)
		err := ValidateActivity(p)
		if err == nil || !errors.Is(err, ErrInvalidPage) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want refusal naming %q", c.name, err, c.want)
		}
		if err2 := Validate(p); err2 == nil {
			t.Errorf("%s: Validate accepted what ValidateActivity refused", c.name)
		}
	}
	if n := len(auditRules) + len(usageRules) + len(factRules) + len(snapshotRules) + len(retractionRules); n != 33 {
		t.Fatalf("activity tables have %d rows; add a case per new row", n)
	}
}

// TestShapeDigestWildcardsDynamicKeys and TestReceiptShapeDigestHasNoValues
// (plan 12A.7): a digest records schema paths and JSON types only. Unknown
// members and map keys (emails, secrets, repository names, ids) are
// wildcarded; values never change it.
func TestShapeDigestWildcardsDynamicKeys(t *testing.T) {
	known := KnownSet([]string{"seats", "seats[]", "seats[].login", "total_seats"})
	a, b := Shape{}, Shape{}
	a.AddBody([]byte(`{"total_seats":1,"seats":[{"login":"octo","dev@corp.example":{"x":1}}]}`), known)
	b.AddBody([]byte(`{"total_seats":99,"seats":[{"login":"other","acme/private-repo":{"x":2}}]}`), known)
	if a.Digest() == "" || a.Digest() != b.Digest() {
		t.Fatalf("digests differ for the same shape: %v vs %v", a, b)
	}
	for k := range a {
		for _, leak := range []string{"dev@corp", "octo", "private-repo", "\"x\""} {
			if strings.Contains(k, leak) {
				t.Errorf("shape entry %q carries %q", k, leak)
			}
		}
	}
	c := Shape{}
	c.AddBody([]byte(`{"total_seats":"1","seats":[]}`), known)
	if c.Digest() == a.Digest() {
		t.Error("a JSON type change must change the digest")
	}
	d := Shape{}
	d.AddBody([]byte(`not json`), known)
	if !d["$:invalid"] {
		t.Error("an undecodable body is recorded as invalid")
	}
	if (Shape{}).Digest() != "" {
		t.Error("an empty shape has no digest")
	}
}
