//go:build sbo_omit_org_update

package main

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestUpdateOmittedBuild pins a -tags sbo_omit_org_update build (lane
// MP-GOV4, T-711): the binary reports org.update compiled out, `observer
// update` refuses with exit code 3 and the "not in this build" line, the
// daemon wires no updater (every call site start.go makes is a nil-safe
// no-op), and the proxy still gets a usable drain gate.
func TestUpdateOmittedBuild(t *testing.T) {
	if !slices.Contains(components.Omitted(), "org.update") {
		t.Fatalf("org.update is not reported omitted: %v", components.Omitted())
	}
	cmd := newUpdateCmd()
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"status", "--json"})
	err := cmd.Execute()
	var ee exitErr
	if !errors.As(err, &ee) || int(ee) != componentstate.RefusalExitCode {
		t.Fatalf("err = %v, want exit code %d", err, componentstate.RefusalExitCode)
	}
	if !strings.Contains(errBuf.String(), "Self-update (org.update) is not in this build") {
		t.Errorf("stderr = %q", errBuf.String())
	}

	rt := updateRuntimeFor(config.Config{}, "", nil, nil, nil, nil)
	if rt != nil || currentUpdateRuntime() != nil || handshakeChildNonce() != "" {
		t.Fatal("a build without org.update must wire no updater")
	}
	if rt.ApplySeam() != nil || rt.StatusSeam() != nil || rt.ExtensionVersionSeam() != nil {
		t.Fatal("the dashboard seams must be nil")
	}
	ctx := context.Background()
	rt.PublishPosture()
	rt.RecoverAtBoot(ctx)
	rt.ReportChildSelfCheck(ctx)
	rt.AutoApplyLoop(ctx)
	rt.SetReleaseListeners(func() { t.Fatal("no apply may release the listeners") })

	if g := updateDrainGate(); g == nil {
		t.Fatal("the proxy's drain gate must exist in every build")
	}
}
