package opencode

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

func fixture(t *testing.T, vendor, surface string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", vendor, surface, "normal_page.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestBudgets (lane ENTAPI-L16C, T-710): the documented example and the two
// synthesized members land one snapshot each under the e-mail; the limit
// state reads custom, default and unlimited; microcents stay exact strings;
// every field-policy row is schema drift.
func TestBudgets(t *testing.T) {
	p, err := parseBudgets(vendorapi.Response{Status: 200, Body: fixture(t, "opencode", string(SurfaceBudgets))})
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Snapshots) != 3 {
		t.Fatalf("snapshots %+v", p.Snapshots)
	}
	a, b, c := p.Snapshots[0], p.Snapshots[1], p.Snapshots[2]
	if a.Class != BudgetSnapshotClass || a.Subject != "alice@example.com" || a.Body["limit_state"] != "custom" ||
		a.Body["limit_micro_cents"] != "7525000000" || a.Body["spent_micro_cents"] != "1250000000" ||
		a.Body["resets_at"] != "2026-10-01T00:00:00Z" || a.Body["updated_at"] != "2026-09-03T12:00:00Z" {
		t.Errorf("alice %+v", a)
	}
	if b.Body["limit_state"] != "default" || b.Body["exceeded"] != "true" || b.Body["updated_at"] != "" {
		t.Errorf("bob %+v", b)
	}
	if c.Body["limit_state"] != "unlimited" || c.Body["limit_micro_cents"] != "" {
		t.Errorf("carol %+v", c)
	}
	base := `"user_id":"u1","email":"x@example.com","spent_micro_cents":"1","exceeded":false,"resets_at":"2026-10-01T00:00:00Z"`
	for name, body := range map[string]string{
		"not an array":        `{"members":[]}`,
		"null body":           `null`,
		"email missing":       `[{"user_id":"u1","spent_micro_cents":"1","exceeded":false,"resets_at":"2026-10-01T00:00:00Z","limit_micro_cents":null,"source":null}]`,
		"spent number":        `[{"user_id":"u1","email":"x@example.com","spent_micro_cents":1,"exceeded":false,"resets_at":"2026-10-01T00:00:00Z","limit_micro_cents":null,"source":null}]`,
		"spent fraction":      `[{"user_id":"u1","email":"x@example.com","spent_micro_cents":"1.5","exceeded":false,"resets_at":"2026-10-01T00:00:00Z","limit_micro_cents":null,"source":null}]`,
		"limit without src":   `[{` + base + `,"limit_micro_cents":"5","source":null}]`,
		"src without limit":   `[{` + base + `,"limit_micro_cents":null,"source":"custom"}]`,
		"source undocumented": `[{` + base + `,"limit_micro_cents":"5","source":"team"}]`,
		"limit negative":      `[{` + base + `,"limit_micro_cents":"-5","source":"custom"}]`,
		"reset time":          `[{"user_id":"u1","email":"x@example.com","spent_micro_cents":"1","exceeded":false,"resets_at":"October","limit_micro_cents":null,"source":null}]`,
		"repeated member":     `[{` + base + `,"limit_micro_cents":null,"source":null},{` + base + `,"limit_micro_cents":null,"source":null}]`,
		"user id path":        `[{"user_id":"../u","email":"x@example.com","spent_micro_cents":"1","exceeded":false,"resets_at":"2026-10-01T00:00:00Z","limit_micro_cents":null,"source":null}]`,
	} {
		if _, err := parseBudgets(vendorapi.Response{Status: 200, Body: []byte(body)}); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	if req, err := buildBudgets(nil, vendorapi.Window{}, ""); err != nil || req.Path != "/console/api/v1/budgets/members" || len(req.Query) != 0 {
		t.Errorf("request %+v %v", req, err)
	}
}

// TestHyperCredits (lane ENTAPI-L16C, T-710): the documented balance lands as
// one team snapshot in hypercredits; a quoted, exponent or missing balance is
// schema drift.
func TestHyperCredits(t *testing.T) {
	p, err := parseHyper(vendorapi.Response{Status: 200, Body: fixture(t, "charm", string(SurfaceHyperCredits))})
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Snapshots) != 1 || p.Snapshots[0].Body["balance"] != "100" || p.Snapshots[0].Subject != HyperSubject ||
		p.Snapshots[0].Body["unit"] != "hypercredits" {
		t.Fatalf("snapshot %+v", p.Snapshots)
	}
	if p, err := parseHyper(vendorapi.Response{Status: 200, Body: []byte(`{"balance":-12.25}`)}); err != nil ||
		p.Snapshots[0].Body["balance"] != "-12.25" {
		t.Errorf("negative fractional balance %+v %v", p, err)
	}
	for name, body := range map[string]string{
		"missing": `{}`, "null": `{"balance":null}`, "quoted": `{"balance":"100"}`, "exponent": `{"balance":1e3}`,
	} {
		if _, err := parseHyper(vendorapi.Response{Status: 200, Body: []byte(body)}); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
}
