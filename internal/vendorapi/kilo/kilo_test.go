package kilo

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

const orgID = "6f1e0c2a-3b4d-4e5f-8a9b-0c1d2e3f4a5b"

func fixture(t *testing.T, s vs.SurfaceID, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "kilo", string(s), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func resp(body []byte) vendorapi.Response {
	return vendorapi.Response{Status: 200, Origin: "https://app.kilo.ai:443", Body: body}
}

func wantDrift(t *testing.T, name string, err error) {
	t.Helper()
	if err == nil || !errors.Is(err, vendorapi.ErrSchemaDrift) {
		t.Errorf("%s: err %v, want schema drift", name, err)
	}
}

// TestTRPCEncoderIsClosed (lane ENTAPI-L16C, T-710): the encoder emits only
// the closed input's fields as the single input parameter, the documented
// required members included, and the request matches the corpus fixture.
func TestTRPCEncoderIsClosed(t *testing.T) {
	w := vendorapi.Window{From: time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	req, err := buildTimeseries(vendorapi.Params{ParamOrganization: orgID, "models": "x"}, w, "")
	if err != nil {
		t.Fatal(err)
	}
	if req.Path != "/api/trpc/usageAnalytics.getTimeseries" || len(req.Query) != 1 || len(req.Query["input"]) != 1 {
		t.Fatalf("request %+v", req)
	}
	var in map[string]any
	if err := json.Unmarshal([]byte(req.Query["input"][0]), &in); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"startDate": "2026-10-06T00:00:00Z", "endDate": "2026-10-07T12:00:00Z", "granularity": "day", "costSource": "cost",
		"organizationId": orgID, "personalScope": "personal-only", "viewAs": "org-wide", "metric": "tokens",
	}
	if len(in) != len(want) {
		t.Fatalf("input has %d members, want %d: %v", len(in), len(want), in)
	}
	for k, v := range want {
		if in[k] != v {
			t.Errorf("input %s = %v, want %v", k, in[k], v)
		}
	}
	var side struct {
		HTTP struct {
			Query map[string]string `json:"query"`
		} `json:"http"`
	}
	if err := json.Unmarshal(fixture(t, SurfaceTimeseries, "normal_page.source.json"), &side); err != nil {
		t.Fatal(err)
	}
	if side.HTTP.Query["input"] != req.Query["input"][0] {
		t.Errorf("encoded input %s differs from the corpus request %s", req.Query["input"][0], side.HTTP.Query["input"])
	}
	if got := url.Values(req.Query).Encode(); got == "" {
		t.Error("empty query")
	}
	for _, o := range []string{"", "org_xxx", "00000000-0000-0000-0000-000000000000", orgID + "x"} {
		if _, err := buildTimeseries(vendorapi.Params{ParamOrganization: o}, w, ""); err == nil {
			t.Errorf("organization %q accepted", o)
		}
		if _, err := buildMembers(vendorapi.Params{ParamOrganization: o}, w, ""); err == nil {
			t.Errorf("members organization %q accepted", o)
		}
	}
}

// TestTimeseriesIsAnUnattributedGatewayFact (lane ENTAPI-L16C, T-710).
func TestTimeseriesIsAnUnattributedGatewayFact(t *testing.T) {
	if d := Timeseries(); d.Tool != "gateway:kilo" || !vendorapi.IsGatewayTool(d.Tool) {
		t.Fatalf("tool %q", d.Tool)
	}
	if _, ok := integration.For(Tool); ok {
		t.Fatal("the gateway attribution names a registry tool")
	}
	p, err := parseTimeseries(resp(fixture(t, SurfaceTimeseries, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.ValidateActivity(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Facts) != 2 || p.Facts[0].ValueMicros != 1_250_000_000_000 || p.Facts[0].IdentityKind != "" ||
		!p.Facts[1].IntervalStart.Equal(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("facts %+v", p.Facts)
	}
	env := func(data string) []byte { return []byte(`{"result":{"data":` + data + `}}`) }
	for name, body := range map[string][]byte{
		"no envelope":  []byte(`{"timeseries":[],"effectiveGranularity":"day"}`),
		"null data":    env(`null`),
		"granularity":  env(`{"timeseries":[],"effectiveGranularity":"week"}`),
		"label":        env(`{"timeseries":[{"datetime":"2026-10-06","value":1,"label":"model-a"}],"effectiveGranularity":"day"}`),
		"fraction":     env(`{"timeseries":[{"datetime":"2026-10-06","value":1.5}],"effectiveGranularity":"day"}`),
		"negative":     env(`{"timeseries":[{"datetime":"2026-10-06","value":-1}],"effectiveGranularity":"day"}`),
		"quoted":       env(`{"timeseries":[{"datetime":"2026-10-06","value":"1"}],"effectiveGranularity":"day"}`),
		"not midnight": env(`{"timeseries":[{"datetime":"2026-10-06T05:00:00Z","value":1}],"effectiveGranularity":"day"}`),
		"repeated":     env(`{"timeseries":[{"datetime":"2026-10-06","value":1},{"datetime":"2026-10-06T00:00:00Z","value":2}],"effectiveGranularity":"day"}`),
		"array root":   []byte(`[{"result":{"data":{}}}]`),
	} {
		_, err := parseTimeseries(resp(body))
		wantDrift(t, name, err)
	}
}

// TestMembersLandsActiveOnly (lane ENTAPI-L16C, T-710): active members land,
// an invited row (no id) is counted, never landed; field policy is drift.
func TestMembersLandsActiveOnly(t *testing.T) {
	p, err := parseMembers(resp(fixture(t, SurfaceMembers, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	p.Snapshot.ManifestID, p.Snapshot.ObservedAt = "run-1", time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	if err := vendorapi.Validate(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Resources) != 2 || p.Resources[0].Attrs["role"] != "owner" || p.Resources[0].DisplayName != "Ada Example" {
		t.Fatalf("members %+v", p.Resources)
	}
	env := func(data string) []byte { return []byte(`{"result":{"data":` + data + `}}`) }
	for name, body := range map[string][]byte{
		"not array": env(`{}`),
		"role":      env(`[{"id":"u","name":"n","email":"e","role":"guest","status":"active","inviteDate":null}]`),
		"status":    env(`[{"id":"u","name":"n","email":"e","role":"member","status":"suspended","inviteDate":null}]`),
		"active id": env(`[{"name":"n","email":"e","role":"member","status":"active","inviteDate":null}]`),
		"repeated":  env(`[{"id":"u","name":"n","email":"e","role":"member","status":"active"},{"id":"u","name":"n","email":"e","role":"member","status":"active"}]`),
		"invite":    env(`[{"email":"e","role":"member","status":"invited","inviteDate":null,"emailStatus":"bounced"}]`),
	} {
		_, err := parseMembers(resp(body))
		wantDrift(t, name, err)
	}
}
