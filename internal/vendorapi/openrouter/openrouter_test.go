package openrouter

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

func fixture(t *testing.T, s vs.SurfaceID, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "openrouter", string(s), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func resp(body []byte, cursor string) vendorapi.Response {
	return vendorapi.Response{Status: 200, Origin: "https://openrouter.ai:443", Body: body, Cursor: cursor}
}

func wantDrift(t *testing.T, name string, err error) {
	t.Helper()
	if err == nil || !errors.Is(err, vendorapi.ErrSchemaDrift) {
		t.Errorf("%s: err %v, want schema drift", name, err)
	}
}

// TestOrgMembersOffsetPaging (lane ENTAPI-L16C, T-710): the offset family
// pages to the terminal page; page one served again at the second offset is
// the paging loop and fails as schema drift; field policy rows are drift.
func TestOrgMembersOffsetPaging(t *testing.T) {
	p, err := parseOrgMembers(resp(fixture(t, SurfaceOrgMembers, "normal_page.json"), ""))
	if err != nil {
		t.Fatal(err)
	}
	p.Snapshot.ManifestID, p.Snapshot.ObservedAt = "run-1", time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	if err := vendorapi.Validate(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Resources) != 2 || p.NextCursor == "" {
		t.Fatalf("page %+v", p)
	}
	jane := p.Resources[0]
	if jane.DisplayName != "Jane Doe" || jane.Attrs["email"] != "jane.doe@example.com" || jane.Attrs["role"] != "org:member" {
		t.Errorf("jane %+v", jane)
	}
	req, err := buildOrgMembers(nil, vendorapi.Window{}, p.NextCursor)
	if err != nil || req.Query["offset"][0] != "2" || req.Query["limit"][0] != "100" {
		t.Fatalf("second request %+v %v", req, err)
	}
	last, err := parseOrgMembers(resp(fixture(t, SurfaceOrgMembers, "last_page.json"), p.NextCursor))
	if err != nil || last.NextCursor != "" || len(last.Resources) != 1 {
		t.Fatalf("last page %+v %v", last, err)
	}
	// The loop: page one again at offset 2.
	_, err = parseOrgMembers(resp(fixture(t, SurfaceOrgMembers, "normal_page.json"), p.NextCursor))
	wantDrift(t, "loop", err)
	// The loop with a total that still covers it: only the repeated-record
	// rule (vendorapi.OffsetLoop) can see it.
	big := []byte(`{"data":[{"id":"u1","email":"a@b","role":"org:member"},{"id":"u2","email":"c@d","role":"org:member"}],"total_count":10}`)
	p1, err := parseOrgMembers(resp(big, ""))
	if err != nil || p1.NextCursor == "" {
		t.Fatalf("big first page %+v %v", p1, err)
	}
	_, err = parseOrgMembers(resp(big, p1.NextCursor))
	wantDrift(t, "loop under a covering total", err)
	for name, body := range map[string]string{
		"data missing":  `{"total_count":1}`,
		"total missing": `{"data":[]}`,
		"total quoted":  `{"data":[],"total_count":"0"}`,
		"role":          `{"data":[{"id":"u","email":"a@b","role":"owner"}],"total_count":1}`,
		"id missing":    `{"data":[{"email":"a@b","role":"org:member"}],"total_count":1}`,
		"repeated":      `{"data":[{"id":"u","email":"a@b","role":"org:member"},{"id":"u","email":"a@b","role":"org:member"}],"total_count":2}`,
		"total short":   `{"data":[{"id":"u","email":"a@b","role":"org:member"}],"total_count":0}`,
		"empty more":    `{"data":[],"total_count":5}`,
	} {
		_, err := parseOrgMembers(resp([]byte(body), ""))
		wantDrift(t, name, err)
	}
	if _, err := buildOrgMembers(nil, vendorapi.Window{}, "2"); err == nil {
		t.Error("a bare offset cursor was accepted")
	}
}

// TestActivityIsAnUnattributedGatewayFact (lane ENTAPI-L16C, T-710): every
// row lands org-level facts under the reserved gateway attribution, which is
// not a registry tool; cached tokens are never subtracted from prompt tokens.
func TestActivityIsAnUnattributedGatewayFact(t *testing.T) {
	d := Activity()
	if !vendorapi.IsGatewayTool(d.Tool) || d.Tool != "gateway:openrouter" {
		t.Fatalf("tool %q", d.Tool)
	}
	if _, ok := integration.For(d.Tool); ok {
		t.Fatal("the gateway attribution names a registry tool")
	}
	p, err := parseActivity(resp(fixture(t, SurfaceActivity, "normal_page.json"), ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.ValidateActivity(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Facts) != 14 || p.SourceRecords != 2 {
		t.Fatalf("facts %d records %d", len(p.Facts), p.SourceRecords)
	}
	got := map[string]int64{}
	for _, f := range p.Facts {
		if f.IdentityKind != "" || f.Identity != "" {
			t.Errorf("fact attributed to an identity: %+v", f)
		}
		if f.ModelRaw == "openai/gpt-4.1" {
			got[f.Metric] = f.ValueMicros
		}
	}
	if got["cost"] != 15000 || got["byok_cost"] != 12000 || got["input_tokens"] != 50_000_000 ||
		got["cache_read_tokens"] != 10_000_000 || got["requests"] != 5_000_000 {
		t.Errorf("metrics %v", got)
	}
	for name, body := range map[string]string{
		"data missing":   `{}`,
		"usage quoted":   `{"data":[{"date":"2025-08-24","model":"m","model_permaslug":"m","endpoint_id":"e","provider_name":"p","usage":"0.1","byok_usage_inference":0,"requests":1,"prompt_tokens":1,"completion_tokens":1,"reasoning_tokens":0,"cached_tokens":0}]}`,
		"negative count": `{"data":[{"date":"2025-08-24","model":"m","model_permaslug":"m","endpoint_id":"e","provider_name":"p","usage":0.1,"byok_usage_inference":0,"requests":-1,"prompt_tokens":1,"completion_tokens":1,"reasoning_tokens":0,"cached_tokens":0}]}`,
		"date":           `{"data":[{"date":"Aug 24","model":"m","model_permaslug":"m","endpoint_id":"e","provider_name":"p","usage":0.1,"byok_usage_inference":0,"requests":1,"prompt_tokens":1,"completion_tokens":1,"reasoning_tokens":0,"cached_tokens":0}]}`,
		"missing tokens": `{"data":[{"date":"2025-08-24","model":"m","model_permaslug":"m","endpoint_id":"e","provider_name":"p","usage":0.1,"byok_usage_inference":0,"requests":1}]}`,
	} {
		_, err := parseActivity(resp([]byte(body), ""))
		wantDrift(t, name, err)
	}
	if _, err := buildActivity(nil, vendorapi.Window{}, "x"); err == nil {
		t.Error("a paged activity request was built")
	}
}

// TestCreditsSnapshot (lane ENTAPI-L16C, T-710): the balance is one snapshot
// in micro-dollars.
func TestCreditsSnapshot(t *testing.T) {
	p, err := parseCredits(resp(fixture(t, SurfaceCredits, "normal_page.json"), ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Snapshots) != 1 || p.Snapshots[0].Body["total_credits"] != "100500000" || p.Snapshots[0].Body["total_usage"] != "25750000" {
		t.Fatalf("snapshot %+v", p.Snapshots)
	}
	for name, body := range map[string]string{
		"data missing": `{}`, "quoted": `{"data":{"total_credits":"1","total_usage":0}}`,
		"negative": `{"data":{"total_credits":-1,"total_usage":0}}`, "exponent": `{"data":{"total_credits":1e3,"total_usage":0}}`,
	} {
		_, err := parseCredits(resp([]byte(body), ""))
		wantDrift(t, name, err)
	}
}
