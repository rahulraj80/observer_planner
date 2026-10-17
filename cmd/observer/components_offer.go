package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/componentsvc"
)

// components_offer.go: a verb whose component is installed but inactive can
// offer to turn it on, on a terminal, instead of only refusing (T-734,
// D-547). `observer service install --now` is the documented way to keep
// Observer running, yet integration.service is off in every distro, so the
// first thing an operator who followed the docs saw was a refusal.
//
// The offer is a table (offerOnUse): only a component listed there offers,
// so the refusal stays the default for every other verb. It never offers on
// a pipe, for a component the build lacks or a policy blocks, or on a node
// whose organization governs the selection (the add would be a local widening
// that the org may refuse; the refusal line already names the fix).

// offerOnUse lists the components whose verb offers to turn them on.
var offerOnUse = map[string]bool{
	"integration.service": true,
}

// componentActivator turns a component on; a seam so tests need no config.
var componentActivator = func(ctx context.Context, cfgPath, id string) error {
	svc, err := newComponentsService(cfgPath, nil)
	if err != nil {
		return err
	}
	ch := componentsvc.Change{Add: []string{id}}
	pv, err := svc.Preview(ctx, ch)
	if err != nil {
		return err
	}
	_, err = svc.Apply(ctx, ch, componentsvc.ApplyOptions{ExpectFingerprint: pv.Fingerprint, Principal: "cli"})
	return err
}

// interactiveVerb reports whether the verb may prompt; a seam for tests.
var interactiveVerb = isInteractiveTerminal

// offerActivation explains the inactive component, asks, and on a yes turns
// it on. It reports true only when the component is now active.
func offerActivation(cmd *cobra.Command, snap componentstate.Snapshot, id, cfgPath string) bool {
	if !offerOnUse[id] || snap.Governed.OrgManaged || !interactiveVerb(cmd) {
		return false
	}
	if d, ok := snap.Decision(id); !ok || d.State != components.StateAvailable {
		return false
	}
	title := id
	if c, ok := components.Get(id); ok && c.Title != "" {
		title = c.Title
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s (%s) is installed but not turned on, so this command would do nothing it is meant to.\n", title, id)
	ok, err := promptYesNo(cmd, fmt.Sprintf("Turn on %s now (the same as `observer components add %s`)? [y/N] ", id, id))
	if err != nil || !ok {
		return false
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := componentActivator(ctx, cfgPath, id); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "components: could not turn on %s: %v\n", id, err)
		return false
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "components: %s is on.\n", id)
	return true
}
