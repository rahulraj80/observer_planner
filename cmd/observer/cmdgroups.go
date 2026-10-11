package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Command groups and unknown subcommands (T-757, generalising T-746).
//
// cobra hands a group command (one with subcommands) the words it could not
// resolve to a subcommand as positional arguments. With no Args validator a
// non-root group accepts them, and a group with no Run of its own then prints
// help and exits 0, so `observer service instal` "succeeded" without doing
// anything. guardCommandGroups walks the finished tree once and gives every
// group that did not choose its own Args the same refusal: an error naming the
// unknown word, a suggestion when cobra has one, and the group's --help, which
// main() turns into exit 1. A group that declares its own Args (it takes a
// positional value, or words a domain hint like `observer org`) keeps it.

// annotationGroupGuard marks a group guardCommandGroups gave the
// unknown-subcommand validator; annotationHelpOnlyGroup marks a group that
// has no action of its own (it only prints help). Tests and the installer
// command check read them.
const (
	annotationGroupGuard    = "observer.group.unknown-subcommand-guard"
	annotationHelpOnlyGroup = "observer.group.help-only"
)

// unknownSubcommandArgs refuses any positional argument a group command
// receives: for a group, a leftover word is a subcommand cobra could not find.
func unknownSubcommandArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	hint := "run `" + cmd.CommandPath() + " --help` for its subcommands"
	if s := cmd.SuggestionsFor(args[0]); len(s) > 0 {
		hint = "did you mean `" + cmd.CommandPath() + " " + s[0] + "`? (" + hint + ")"
	}
	return fmt.Errorf("unknown command %q for %q: %s", args[0], cmd.CommandPath(), hint)
}

// guardCommandGroups applies unknownSubcommandArgs to every group below root
// whose Args is unset, and gives a group with no action of its own a RunE
// that prints help (cobra checks runnability before it validates arguments,
// so a group without a Run would otherwise print help and exit 0 before the
// validator is ever consulted). The root keeps cobra's own root check.
//
// cobra adds its `completion` group only when the command executes; it is
// added here first (Execute then finds it present and keeps it) so it gets
// the same treatment: its own NoArgs validator stays, and it gains the
// help RunE that lets that validator run.
func guardCommandGroups(root *cobra.Command) {
	root.InitDefaultCompletionCmd()
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			walk(sub)
		}
		if c == root || !c.HasSubCommands() {
			return
		}
		if c.Annotations == nil {
			c.Annotations = map[string]string{}
		}
		if c.Args == nil {
			c.Args = unknownSubcommandArgs
			c.Annotations[annotationGroupGuard] = "true"
		}
		if !c.Runnable() {
			c.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
			c.Annotations[annotationHelpOnlyGroup] = "true"
		}
	}
	walk(root)
}
