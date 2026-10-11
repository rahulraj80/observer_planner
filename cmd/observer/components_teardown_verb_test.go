package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentsvc"
)

// TestTeardownVerbRunsWhileItsComponentIsInactive pins T-681: removing
// agentaccess.relay must not refuse `observer mcp unwrap`, the command its
// own teardown step tells the operator to run, while every other verb of
// the inactive component stays refused (exit code 3). A step that names
// flags matches only when they are set: `routing apply --tool X --revert`
// runs, a plain `routing apply` (a write) does not.
func TestTeardownVerbRunsWhileItsComponentIsInactive(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[components]\nremove = [\"agentaccess.relay\", \"proxy.routing\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ran := map[string]int{}
	build := func() *cobra.Command {
		root := &cobra.Command{Use: "observer"}
		leaf := func(use string) *cobra.Command {
			c := &cobra.Command{Use: use, RunE: func(cmd *cobra.Command, _ []string) error {
				ran[strings.TrimPrefix(cmd.CommandPath(), "observer ")]++
				return nil
			}}
			var p string
			c.Flags().StringVar(&p, "config", "", "")
			return c
		}
		mcp := &cobra.Command{Use: "mcp"}
		mcp.AddCommand(leaf("unwrap"), leaf("status"))
		routing := &cobra.Command{Use: "routing"}
		apply := leaf("apply")
		var tool string
		var revert bool
		apply.Flags().StringVar(&tool, "tool", "", "")
		apply.Flags().BoolVar(&revert, "revert", false, "")
		routing.AddCommand(apply)
		root.AddCommand(mcp, routing)
		gateComponentVerbs(root)
		return root
	}
	cases := []struct {
		args    []string
		allowed bool
	}{
		{[]string{"mcp", "unwrap"}, true},
		{[]string{"mcp", "status"}, false},
		{[]string{"routing", "apply", "--tool", "claude-code", "--revert"}, true},
		{[]string{"routing", "apply", "--tool", "claude-code"}, false},
	}
	for _, tc := range cases {
		root := build()
		var errBuf bytes.Buffer
		root.SetErr(&errBuf)
		root.SetArgs(append(tc.args, "--config", cfgPath))
		err := root.Execute()
		key := strings.Join(tc.args[:2], " ")
		if tc.allowed {
			if err != nil || ran[key] != 1 {
				t.Fatalf("%v: teardown verb refused while inactive: err=%v ran=%d stderr=%q", tc.args, err, ran[key], errBuf.String())
			}
			ran[key] = 0
			continue
		}
		var ec exitErr
		if !errors.As(err, &ec) || int(ec) != 3 || ran[key] != 0 {
			t.Fatalf("%v: want refusal with exit 3, got err=%v ran=%d", tc.args, err, ran[key])
		}
	}
}

// TestTeardownCommandsExistInTheCommandTree keeps every manual teardown
// step honest: the command it names is a real verb path of this binary and
// every flag it sets exists on that verb, so the gate exemption above can
// match it (catalog truth, plan 3.2 item 4).
func TestTeardownCommandsExistInTheCommandTree(t *testing.T) {
	root := newRootCmdWith(usageDeps{})
	checked := 0
	for _, c := range components.ForBinary(components.BinaryNode) {
		for _, st := range componentsvc.TeardownFor(c.ID) {
			if st.Kind != componentsvc.StepManual || st.Command == "" {
				continue
			}
			path, flags := teardownCommandShape(st.Command)
			if compiledOut(c.ID) && len(path) > 0 {
				// The component is compiled out of this build: its top verb is
				// the compiled-out stand-in (compiledOutVerb, "not in this
				// build", exit 3) with no subcommands, so only the top verb
				// must exist (D-535).
				if top, _, err := root.Find(path[:1]); err != nil || top == root {
					t.Errorf("%s: compiled-out component has no %q verb answering \"not in this build\" (err %v)", c.ID, path[0], err)
				}
				checked++
				continue
			}
			cmd, rest, err := root.Find(path)
			if err != nil || len(rest) != 0 || cmd == root {
				t.Errorf("%s: teardown command %q is not a verb of this binary (rest %v, err %v)", c.ID, st.Command, rest, err)
				continue
			}
			for _, f := range flags {
				if cmd.Flags().Lookup(f) == nil {
					t.Errorf("%s: teardown command %q sets --%s, which %q does not have", c.ID, st.Command, f, cmd.CommandPath())
				}
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no manual teardown step found; the table moved")
	}
}
