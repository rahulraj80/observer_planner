package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
)

// compiledOutVerb is the verb a *_omit.go wiring file registers for a
// component compiled out of this binary (L1). gateComponentVerbs wraps it
// like any verb of an inactive component, so the refusal normally comes
// from the gate; this RunE is the same refusal for a harness that skips the
// gate. Exit code 3 (componentstate.RefusalExitCode).
func compiledOutVerb(use, short, id string) *cobra.Command {
	return &cobra.Command{
		Use:                use,
		Short:              short,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			title := id
			if c, ok := components.Get(id); ok && c.Title != "" {
				title = c.Title + " (" + id + ")"
			}
			// D-514: name the build and the fix.
			fmt.Fprintf(cmd.ErrOrStderr(), "%s is not in this build; %s. Install a build that includes it (%s).\n", title, components.DistrosHaving(id), components.SettingsHint())
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			return exitErr(componentstate.RefusalExitCode)
		},
	}
}
