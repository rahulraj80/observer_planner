//go:build !sbo_omit_org_update

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/govern"
	"github.com/marmutapp/superbased-observer/internal/orgclient"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// T-727 (D-541): on demo node 1 `observer update status` printed "auto-apply
// off - the default for a BYO node" while the daemon log and the org Fleet row
// said on (the managed-fleet default). The CLI built its own posture from
// admin_managed alone; the daemon's comes from the org client's ShareOptions,
// whose EnterpriseGranted is the live enrolment grant. Both now go through
// updateManagedPosture + resolveUpdateAutoApply, the CLI with
// cliUpdateShareOptions. This pins that the two agree in both tenancies.
func TestT727UpdateStatusAgreesWithDaemonByTenancy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tenancy  string
		wantOn   bool
		wantWhy  string
		notInWhy string
	}{
		{"managed enrolment with extract.managed", orgcontract.TenancyManaged, true, "enterprise-posture", "BYO"},
		{"individual enrolment", orgcontract.TenancyIndividual, false, "BYO", "managed-fleet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, _, _ := signedGrantFixture(t, tc.tenancy, []string{govern.AuthorityExtractManaged})
			path, _ := w4fMetadataOnlyConfig(t)
			cfg, err := config.Load(config.LoadOptions{GlobalPath: path})
			if err != nil {
				t.Fatalf("config.Load: %v", err)
			}

			// The daemon: the org client's ShareOptions, which
			// lowerShareOptions builds from the live grant.
			daemonShare := lowerShareOptions(orgclient.ShareOptionsFromConfig(cfg.OrgClient), enterpriseContentEffective(ctx, cfg, st, nil))
			daemonOn, daemonWhy := resolveUpdateAutoApply(cfg, updateManagedPosture(daemonShare))

			// The CLI.
			cliOn, cliWhy := resolveUpdateAutoApply(cfg, updateManagedPosture(cliUpdateShareOptions(ctx, cfg, st, nil)))

			if cliOn != daemonOn || cliWhy != daemonWhy {
				t.Fatalf("update status says (%v, %q) but the daemon says (%v, %q)", cliOn, cliWhy, daemonOn, daemonWhy)
			}
			if cliOn != tc.wantOn {
				t.Errorf("auto-apply = %v, want %v (%s)", cliOn, tc.wantOn, cliWhy)
			}
			if !strings.Contains(cliWhy, tc.wantWhy) || strings.Contains(cliWhy, tc.notInWhy) {
				t.Errorf("reason %q, want it to name %q and not %q", cliWhy, tc.wantWhy, tc.notInWhy)
			}
		})
	}
}

// TestT727ExplicitConfigWinsOnBothSurfaces: an explicit [update].auto_apply in
// the node's own config wins over the managed default on the CLI exactly as on
// the daemon.
func TestT727ExplicitConfigWinsOnBothSurfaces(t *testing.T) {
	ctx := context.Background()
	st, _, _ := signedGrantFixture(t, orgcontract.TenancyManaged, []string{govern.AuthorityExtractManaged})
	path, _ := w4fMetadataOnlyConfig(t)
	cfg, err := config.Load(config.LoadOptions{GlobalPath: path})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	off := false
	cfg.Update.AutoApply = &off
	on, why := resolveUpdateAutoApply(cfg, updateManagedPosture(cliUpdateShareOptions(ctx, cfg, st, nil)))
	if on || !strings.Contains(why, "set explicitly") {
		t.Fatalf("explicit auto_apply = false on a managed node resolved to (%v, %q)", on, why)
	}
}
