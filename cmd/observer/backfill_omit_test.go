package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	adapterdefaults "github.com/marmutapp/superbased-observer/internal/adapter/defaults"
)

// TestSkipOmittedAdapterPasses: `backfill --all` switches off exactly the
// passes whose adapter the build does not contain, and says so; an
// explicitly named pass refuses with an error a composite run can
// recognise (lane MP-L1D, T-598).
func TestSkipOmittedAdapterPasses(t *testing.T) {
	all := adapterdefaults.Adapters()
	if len(all) == 0 {
		t.Skip("this build compiles every adapter out")
	}
	present := all[0].Name()
	on1, on2 := true, true
	var out bytes.Buffer
	skipOmittedAdapterPasses([]backfillAllPass{
		{&on1, "--present-pass", present},
		{&on2, "--absent-pass", "no-such-adapter"},
	}, &out)
	if !on1 {
		t.Errorf("the pass of a compiled-in adapter (%s) was switched off", present)
	}
	if on2 {
		t.Error("the pass of an adapter not in this build stayed on")
	}
	if !strings.Contains(out.String(), "skipping --absent-pass (the no-such-adapter adapter is not in this build)") {
		t.Errorf("skip not reported: %q", out.String())
	}
	err := adapterPassNotInBuild("--absent-pass", "no-such-adapter")
	if !errors.Is(err, errAdapterNotInBuild) {
		t.Errorf("errors.Is(%v, errAdapterNotInBuild) = false", err)
	}
	if got := err.Error(); got != "--absent-pass: the no-such-adapter adapter is not in this build (observer components list)" {
		t.Errorf("refusal = %q", got)
	}
}
