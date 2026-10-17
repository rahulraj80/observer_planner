package main

import (
	"fmt"

	"github.com/marmutapp/superbased-observer/internal/browserhost"
	"github.com/marmutapp/superbased-observer/internal/componentsvc"
)

// components_browserhost.go is the browser native-messaging host remover
// (modular packaging plan 6.3, ledger MODPKG-5): the one composition
// `observer components remove capture.browser`, `components teardown` and
// the dashboard share. It lives in cmd/ so no internal package outside the
// capture.browser component imports internal/browserhost (the component
// package fence, tests/invariant/components_fence_test.go).

// browserHostRemoveOptions parameterize removeBrowserHost.
type browserHostRemoveOptions struct {
	// HomeDir pins the home (tests). "" = the real home.
	HomeDir string
	// GOOS overrides the OS of the dir-based manifests (tests).
	GOOS string
	// WindowsHomes are the cross-mounted Windows profiles whose registry
	// keys and bridge files are removed (a WSL daemon). Injected, never
	// read from the environment here.
	WindowsHomes []string
	// NewRegistry builds the registry writer for the Windows half; it must
	// also implement browserhost.RegistryDeleter. Nil leaves the Windows
	// keys in place and says so.
	NewRegistry func() (browserhost.RegistryWriter, error)
}

// removeBrowserHost removes the dir-based manifests and host files, then for
// each Windows home the registry keys (only when they name Observer's own
// manifest) and the bridge files, and reports it as one teardown outcome.
func removeBrowserHost(opts browserHostRemoveOptions, dryRun bool) componentsvc.StepOutcome {
	o := componentsvc.StepOutcome{Target: "browser native-messaging host"}
	var results []browserhost.RemoveResult
	addErr := func(e error) {
		if o.Error != "" {
			o.Error += "; "
		}
		o.Error += e.Error()
	}
	reg, err := browserhost.NewRegistrar(browserhost.Options{Home: opts.HomeDir, GOOS: opts.GOOS, DryRun: dryRun})
	if err != nil {
		addErr(err)
	} else {
		results = append(results, reg.Remove()...)
	}
	if len(opts.WindowsHomes) > 0 {
		switch {
		case opts.NewRegistry == nil:
			addErr(fmt.Errorf("no registry writer for the Windows browser keys; they were left in place"))
		default:
			rw, err := opts.NewRegistry()
			if err != nil {
				addErr(err)
				break
			}
			for _, h := range opts.WindowsHomes {
				res, err := browserhost.RemoveWindows(browserhost.WindowsRemoveOptions{WindowsHome: h, Registry: rw, DryRun: dryRun})
				if err != nil {
					addErr(err)
					continue
				}
				results = append(results, res...)
			}
		}
	}
	for _, x := range results {
		switch {
		case x.Error != nil:
			addErr(x.Error)
		case x.Removed:
			o.Changed = true
			o.Detail = append(o.Detail, x.Target+": "+x.Path)
		case x.NeedsAttention != "":
			o.NeedsAttention = append(o.NeedsAttention, x.Path+": "+x.NeedsAttention)
		}
	}
	o.NeedsAttention = append(o.NeedsAttention, "the browser extension itself keeps running in the browser; remove it from the browser's extensions page")
	return o
}
