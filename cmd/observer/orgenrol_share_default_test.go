package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/govern"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestEnrolShareDefaultFollowsTenancyOnBothRails pins the standing ruling
// (CLAUDE.md "ENTERPRISE / ORG-MANAGED NODES SHIP FULL CONTENT"): a managed
// enrolment writes [org_client.share].full_content = true and an individual
// one false, and the dashboard enrol and `observer org enroll` cannot
// diverge - both go through ensureOrgClientBlock with the enrolment's own
// tenancy (EnrollResponse.Tenancy, carried on store.Enrolment), so the block
// each writes is byte-equal and loads to the same posture.
func TestEnrolShareDefaultFollowsTenancyOnBothRails(t *testing.T) {
	cases := []struct {
		tenancy         string
		wantFullContent bool
	}{
		{orgcontract.TenancyManaged, true},
		{orgcontract.TenancyIndividual, false},
	}
	for _, c := range cases {
		t.Run(c.tenancy, func(t *testing.T) {
			ctx := context.Background()
			org := newEnrolFakeOrg(t, c.tenancy, []string{govern.AuthorityDashboardVisibility})

			// The dashboard rail.
			d, _, uiCfg := testEnrolDashboard(t)
			out, err := d.Enrol(ctx, dashapi.EnrolRequest{Method: "code", OrgURL: org.url, Token: idpTestEnrolCode})
			if err != nil {
				t.Fatalf("dashboard Enrol: %v", err)
			}
			if out.Managed != (c.tenancy == orgcontract.TenancyManaged) || out.FullContent != c.wantFullContent || !out.BlockWritten {
				t.Fatalf("dashboard outcome managed=%v full_content=%v block_written=%v", out.Managed, out.FullContent, out.BlockWritten)
			}
			assertLoadedFullContent(t, uiCfg, c.wantFullContent)

			// The CLI rail: the exact call `observer org enroll` makes, with
			// the same tenancy the enrolment reported.
			cliCfg := filepath.Join(t.TempDir(), "config.toml")
			enr := &store.Enrolment{Tenancy: c.tenancy}
			if added, werr := ensureOrgClientBlock(cliCfg, out.OrgServerURL, enr.IsManaged()); werr != nil || !added {
				t.Fatalf("CLI ensureOrgClientBlock = (%v, %v)", added, werr)
			}
			assertLoadedFullContent(t, cliCfg, c.wantFullContent)

			if ui, cli := orgClientBlock(t, uiCfg), orgClientBlock(t, cliCfg); ui != cli {
				t.Fatalf("the dashboard and CLI wrote different [org_client] blocks:\n--- dashboard\n%s\n--- cli\n%s", ui, cli)
			}
		})
	}
}

func assertLoadedFullContent(t *testing.T, path string, want bool) {
	t.Helper()
	cfg, err := config.Load(config.LoadOptions{GlobalPath: path, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("config.Load(%s): %v", path, err)
	}
	if cfg.OrgClient.Share.FullContent != want {
		t.Fatalf("%s: [org_client.share].full_content = %v, want %v", path, cfg.OrgClient.Share.FullContent, want)
	}
}

// orgClientBlock returns the appended [org_client] block (it is always
// written at the end of the file).
func orgClientBlock(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(body), "\n[org_client]\n")
	if i < 0 {
		t.Fatalf("%s has no [org_client] block:\n%s", path, body)
	}
	return string(body[i:])
}
