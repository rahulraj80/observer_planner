//go:build !sbo_omit_org_update

package main

import (
	"errors"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/update"
)

// TestRunningUpdateDistroForACustomBuild pins how a custom build identifies
// itself to the update rail (plan 5.3, lane MP-CUST): a prebuilt stamp wins,
// a custom stamp reports custom-<manifest digest>, a broken custom stamp
// reports an id no index can name (fail closed, never full), and an
// unstamped build is full ("").
func TestRunningUpdateDistroForACustomBuild(t *testing.T) {
	digest := "1111111111111111111111111111111111111111111111111111111111111111"
	none := func() components.Distro { return "" }
	custom := func() (components.CustomBuild, bool, error) {
		return components.CustomBuild{ManifestDigest: digest}, true, nil
	}
	cases := []struct {
		name     string
		prebuilt func() components.Distro
		custom   func() (components.CustomBuild, bool, error)
		want     components.Distro
	}{
		{"unstamped", none, func() (components.CustomBuild, bool, error) { return components.CustomBuild{}, false, nil }, ""},
		{"prebuilt", func() components.Distro { return components.DistroMinimal }, custom, components.DistroMinimal},
		{"custom", none, custom, components.Distro(update.CustomDistro(digest))},
		{"broken custom stamp", none, func() (components.CustomBuild, bool, error) {
			return components.CustomBuild{}, true, errors.New("bad stamp")
		}, components.Distro(update.CustomDistro(customStampErrorDigest))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runningUpdateDistroFrom(c.prebuilt, c.custom)
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
	// The custom id resolves to a context whose v1 fallback is refused and
	// whose target stays the custom id (no policy can name a custom distro).
	dc := resolveUpdateDistroContext(updateDistroInputs{Running: components.Distro(update.CustomDistro(digest))})
	if dc.Current != update.CustomDistro(digest) || dc.Target != dc.Current || dc.LegacyFullAllowed || dc.IsTransition() {
		t.Fatalf("custom context %+v", dc)
	}
	dc = resolveUpdateDistroContext(updateDistroInputs{Running: components.Distro(update.CustomDistro(digest)), PolicyDistro: update.CustomDistro(digest)})
	if dc.Target != update.CustomDistro(digest) {
		t.Fatalf("a policy naming a custom id must not be read as a prebuilt target: %+v", dc)
	}
}
