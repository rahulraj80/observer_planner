package prime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

func walletResp(body string) vendorapi.Response {
	return vendorapi.Response{
		Status: 200, Origin: "https://api.primeintellect.ai:443", Body: []byte(body),
		Params: vendorapi.Params{ParamTeam: "65c1e471-205f-4566-9c5a-07198bcdf4ce"},
	}
}

// TestWallet (lane ENTAPI-L16D, T-713): the team wallet lands one balance
// snapshot in micro-dollars with the documented category credits; the
// billing rows and grant labels never land; another currency, another team
// or a malformed amount is drift; the request names the team and asks one row.
func TestWallet(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "prime", string(SurfaceWallet), "normal_page.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := parseWallet(walletResp(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Snapshots) != 1 || p.Snapshots[0].Class != WalletClass || p.Snapshots[0].Subject != "team:65c1e471-205f-4566-9c5a-07198bcdf4ce" {
		t.Fatalf("page %+v", p)
	}
	body := p.Snapshots[0].Body
	want := map[string]string{
		"unit": "usd_micros", "balance": "412750000", "total_billings": "57", "sandbox_credits_remaining": "5000000",
		"credits_remaining_training": "64200000", "credits_remaining_sandboxes": "5000000", "credits_remaining_inference": "0",
	}
	if len(body) != len(want) {
		t.Fatalf("body %v", body)
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("%s = %q, want %q", k, body[k], v)
		}
	}
	for _, v := range body {
		if strings.Contains(v, "Synthetic grant") || strings.Contains(v, "bill_01") {
			t.Fatalf("a billing row or grant label landed: %v", body)
		}
	}
	base := `{"wallet_id":"w","team_id":"65c1e471-205f-4566-9c5a-07198bcdf4ce","balance_usd":-1.5,"currency":"USD","total_billings":0,"recent_billings":[]}`
	if p, err := parseWallet(walletResp(base)); err != nil || p.Snapshots[0].Body["balance"] != "-1500000" {
		t.Fatalf("negative balance (owed): %+v %v", p, err)
	}
	for name, s := range map[string]string{
		"eur":            strings.Replace(base, `"USD"`, `"EUR"`, 1),
		"other team":     strings.Replace(base, `"65c1e471-205f-4566-9c5a-07198bcdf4ce"`, `"team_x"`, 1),
		"quoted balance": strings.Replace(base, `-1.5`, `"-1.5"`, 1),
		"no wallet":      strings.Replace(base, `"wallet_id":"w",`, ``, 1),
		"no rows":        strings.Replace(base, `,"recent_billings":[]`, ``, 1),
		"neg credits":    strings.Replace(base, `}`, `,"credits_remaining_by_category":{"training":-1}}`, 1),
		"frac total":     strings.Replace(base, `"total_billings":0`, `"total_billings":1.5`, 1),
		"renamed member": strings.Replace(base, `}`, `,"credit_grants_renamed":[]}`, 1),
	} {
		if _, err := parseWallet(walletResp(s)); err == nil || !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want drift", name, err)
		}
	}
	unknown := strings.Replace(base, `}`, `,"credits_remaining_by_category":{"compute":3}}`, 1)
	if p, err := parseWallet(walletResp(unknown)); err != nil || len(p.Snapshots[0].Body) != 3 {
		t.Fatalf("an undocumented category landed or failed: %+v %v", p, err)
	}
	req, err := buildWallet(vendorapi.Params{ParamTeam: "65c1e471-205f-4566-9c5a-07198bcdf4ce"}, vendorapi.Window{}, "")
	if err != nil || req.Path != "/api/v1/billing/wallet" || req.Query["teamId"][0] != "65c1e471-205f-4566-9c5a-07198bcdf4ce" || req.Query["limit"][0] != "1" {
		t.Fatalf("request %+v %v", req, err)
	}
	for _, c := range []struct{ team, cursor string }{{"", ""}, {"a/b", ""}, {"team_1", "1"}} {
		if _, err := buildWallet(vendorapi.Params{ParamTeam: c.team}, vendorapi.Window{}, c.cursor); err == nil {
			t.Errorf("team %q cursor %q accepted", c.team, c.cursor)
		}
	}
}
