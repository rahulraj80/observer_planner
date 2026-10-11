package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// commandGroups lists every command below root that has subcommands.
func commandGroups(root *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c != root && c.HasSubCommands() {
			out = append(out, c)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	return out
}

// pathArgs is c's command path below the root as argv words.
func pathArgs(c *cobra.Command) []string {
	var parts []string
	for p := c; p != nil && p.HasParent(); p = p.Parent() {
		parts = append([]string{p.Name()}, parts...)
	}
	return parts
}

// TestEveryCommandGroupRejectsUnknownSubcommand pins T-757: walking the whole
// observer tree, every command group either declares its own Args (a
// positional value, or a domain hint like `observer org`) or carries the
// generic guard, and every guarded group refuses an unknown subcommand with
// an error (exit 1 in main) naming the word and the group's --help, never
// help and exit 0. A help-only group still prints help when called bare.
func TestEveryCommandGroupRejectsUnknownSubcommand(t *testing.T) {
	groups := commandGroups(newRootCmdWith(usageDeps{}))
	if len(groups) < 30 {
		t.Fatalf("only %d command groups found; the walk no longer sees the tree", len(groups))
	}
	guarded, sawCompletion := 0, false
	for _, g := range groups {
		path := g.CommandPath()
		if g.Name() == "completion" {
			sawCompletion = true // cobra's own group, added before the walk
		}
		if g.Args == nil {
			t.Errorf("`%s` is a command group with no Args validator: an unknown subcommand would be accepted", path)
			continue
		}
		ownGuard := g.Annotations[annotationGroupGuard] != ""
		helpOnly := g.Annotations[annotationHelpOnlyGroup] != ""
		if !ownGuard && !helpOnly {
			continue // runnable with its own validator (it takes a positional value)
		}
		if ownGuard {
			guarded++
		}
		args := append(pathArgs(g), "zz-no-such-subcommand")
		root := newRootCmdWith(usageDeps{})
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		err := root.Execute()
		if err == nil {
			t.Errorf("`%s zz-no-such-subcommand` succeeded; output:\n%s", path, out.String())
			continue
		}
		if !strings.Contains(err.Error(), `unknown command "zz-no-such-subcommand"`) {
			t.Errorf("`%s zz-no-such-subcommand`: error %q does not name the unknown word", path, err)
		}
		if ownGuard && !strings.Contains(err.Error(), path+" --help") {
			t.Errorf("`%s zz-no-such-subcommand`: error %q does not point at the group's --help", path, err)
		}
		if !helpOnly {
			continue // runnable on its own: calling it bare does real work
		}
		root = newRootCmdWith(usageDeps{})
		out.Reset()
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(pathArgs(g))
		if err := root.Execute(); err != nil {
			t.Errorf("bare `%s` failed: %v", path, err)
		} else if !strings.Contains(out.String(), "Usage:") {
			t.Errorf("bare `%s` printed no help:\n%s", path, out.String())
		}
	}
	if guarded == 0 {
		t.Fatal("no group carries the generic guard")
	}
	if !sawCompletion {
		t.Error("cobra's completion group is not in the walked tree, so `observer completion <typo>` is unguarded")
	}
}

// TestServiceUnknownSubcommandFails is the reported case: `observer service
// instal` printed help and exited 0. It now fails and suggests `install`.
func TestServiceUnknownSubcommandFails(t *testing.T) {
	root := newRootCmdWith(usageDeps{})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"service", "instal"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("`observer service instal` succeeded; output:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "unknown command \"instal\"") || !strings.Contains(err.Error(), "observer service install") {
		t.Fatalf("error %q does not name the word and suggest `observer service install`", err)
	}
}
