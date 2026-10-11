//go:build !sbo_omit_cloud_intelligence

package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestCloudHelpMatchesTheSchedulers pins G12: `observer cloud` used to call
// itself "manual-only" with "nothing runs on a schedule", although the daemon's
// two opt-in tickers (auto-sync and background enrichment) and the dashboard
// spawn these same commands. The help must say who spawns them and state the
// real auto-sync default.
func TestCloudHelpMatchesTheSchedulers(t *testing.T) {
	cmd := newCloudCmd()
	help := cmd.Short + "\n" + cmd.Long
	for _, stale := range []string{"manual-only", "MANUAL-ONLY", "nothing runs\non a schedule", "nothing runs on a schedule"} {
		if strings.Contains(help, stale) {
			t.Errorf("observer cloud help still says %q:\n%s", stale, help)
		}
	}
	for _, want := range []string{
		"auto_sync",
		"default " + strconv.Itoa(config.CloudAutoSyncDefaultMinutes),
		"dashboard",
		"consent",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("observer cloud help should mention %q:\n%s", want, help)
		}
	}
}
