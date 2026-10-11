//go:build sbo_omit_cloud_intelligence

package main

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
)

// cloud_omit.go stands in for the cloud*.go wiring in a build without
// cloud.intelligence (-tags sbo_omit_cloud_intelligence, lane MP-L1I,
// T-715, ruling D-512): internal/cloudevidence, cloudclient, cloudpop,
// cloudcred and cloudgateway are not linked. internal/cloudcontract stays
// (it is core: the org wire speaks it). `observer cloud` answers "not in
// this build" (exit 3), the dashboard gets no cloud account seams and the
// commit-alignment grade reports the component missing, and the daemon
// starts neither cloud ticker. Nothing here builds an envelope.
func init() { components.MarkOmitted("cloud.intelligence") }

// newCloudCmd registers `observer cloud` as a compiled-out verb.
func newCloudCmd() *cobra.Command {
	return compiledOutVerb("cloud", "Cloud Intelligence (not in this build)", "cloud.intelligence")
}

// CommitAlignmentAvailability reports the commit-alignment grade as
// unavailable, naming the missing component (the dashboard shows the same
// reason the CLI would).
func CommitAlignmentAvailability() (available bool, reason string) {
	return false, "Cloud Intelligence is not in this build"
}

// newCloudAccountSeams returns nil: the dashboard treats nil seams as no
// cloud account surface (the `observer dashboard` embedder precedent).
func newCloudAccountSeams(string) *dashapi.CloudAccountSeams { return nil }

// runCloudAutoSyncFromConfig spawns nothing: there is no `observer cloud
// sync` in this build. Operator rule D-514: when [cloud].auto_sync asks for
// it, the daemon still starts and prints one line naming the build and the
// fix.
func runCloudAutoSyncFromConfig(ctx context.Context, out, _ io.Writer, configPath string) {
	cfg, _, cleanup, err := loadConfigAndDB(ctx, configPath)
	if err != nil {
		return
	}
	defer cleanup()
	if line, off := settingOffLine("cloud.auto_sync", cfg.Cloud.AutoSync, nil); off {
		fmt.Fprintln(out, "  "+line)
	}
}

// runCloudAutoEnrichFromConfig is a no-op: there is no `observer cloud
// consent` to spawn in this build.
func runCloudAutoEnrichFromConfig(context.Context, io.Writer, io.Writer, string) {}
