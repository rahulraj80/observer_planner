package main

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestOrgIntelConsentRecordsHowItWasGiven pins review M3-14: the consent
// record names the real path, not the literal "interactive" for all of
// them. A flag that skipped the prompt is recorded as that flag, and the
// dashboard as the dashboard.
func TestOrgIntelConsentRecordsHowItWasGiven(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name     string
		accept   func(t *testing.T, f *orgIntelFixture)
		wantMode string
	}{
		{"observer intel accept --yes", func(t *testing.T, f *orgIntelFixture) {
			cliAccept(t, f, orgIntelModeYesFlag)
		}, "cli_yes_flag"},
		{"observer org enrol --accept-intel-consent", func(t *testing.T, f *orgIntelFixture) {
			cliAccept(t, f, orgIntelModeEnrolFlag)
		}, "enrol_flag"},
		{"the dashboard Accept button", func(t *testing.T, f *orgIntelFixture) {
			r := httptest.NewRequest("GET", "/api/intel/status", nil)
			st, err := f.d.Status(r)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.d.Accept(r, st.DisclosureDigest); err != nil {
				t.Fatal(err)
			}
		}, "dashboard"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newOrgIntelFixture(t, orgcontract.TenancyIndividual, true)
			tc.accept(t, f)
			h, err := f.st.OrgIntelConsentHistory(ctx, "org-1", 5)
			if err != nil || len(h) != 1 || h[0].ConsentMode != tc.wantMode {
				t.Fatalf("consent history = %+v, %v; want one accept recorded as %q", h, err, tc.wantMode)
			}
		})
	}

	// The dashboard withdraw is recorded as the dashboard, too.
	f := newOrgIntelFixture(t, orgcontract.TenancyIndividual, true)
	cliAccept(t, f, orgIntelModeYesFlag)
	if _, err := f.d.Withdraw(httptest.NewRequest("POST", "/api/intel/withdraw", nil)); err != nil {
		t.Fatal(err)
	}
	if h, _ := f.st.OrgIntelConsentHistory(ctx, "org-1", 5); len(h) != 2 || h[0].ConsentMode != "dashboard" {
		t.Fatalf("withdraw history = %+v", h)
	}
}

// cliAccept runs the CLI accept path with yes set (no prompt), as the --yes
// and --accept-intel-consent flags do.
func cliAccept(t *testing.T, f *orgIntelFixture, yesMode string) {
	t.Helper()
	b, err := f.d.build(context.Background(), f.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&bytes.Buffer{})
	res, err := runIntelAcceptCLI(cmd, b, f.cfgPath, true, yesMode)
	if err != nil || res == nil {
		t.Fatalf("CLI accept = %+v, %v", res, err)
	}
}
