//go:build !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestSandboxRunIDVerbsSayHowToPassADashID: the `observer sandbox` verbs
// that take a run id give the same hint (the flag error comes before the
// verb runs, so nothing is read or called).
func TestSandboxRunIDVerbsSayHowToPassADashID(t *testing.T) {
	cmd := &cobra.Command{Use: "observer", SilenceUsage: true, SilenceErrors: true}
	cmd.AddCommand(newSandboxCmd())
	cmd.SetArgs([]string{"sandbox", "hosted", "delete", "-Xabc"})
	var errb strings.Builder
	cmd.SetErr(&errb)
	cmd.SetOut(&errb)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "goes after --: observer sandbox hosted delete -- <id>") {
		t.Fatalf("hosted delete without -- = %v", err)
	}
}
