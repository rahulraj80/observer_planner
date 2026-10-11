package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// installerCommandPatterns find the observer commands the installers tell a
// person to run: a command in backticks (PowerShell's doubled backticks
// included), a "Next steps" line, and the "leave it first: <command>" hint.
var installerCommandPatterns = []*regexp.Regexp{
	regexp.MustCompile("`observer ([^`\n]+)`"),
	regexp.MustCompile(`[Ss]ay "  observer ([^"#]+)`),
	regexp.MustCompile(`first: observer ([a-z][^"]*)"`),
}

// resolveInstallerCommand walks line (the words after "observer") down the
// cobra tree: every leading plain word must be a subcommand while the command
// reached still has subcommands (a group never takes a positional word, which
// is how `observer org enroll` used to be accepted as help), every flag must
// exist, and the command reached must run (a hidden command only with
// allowHidden: an installer prints the canonical name). It returns "" or
// what is wrong.
func resolveInstallerCommand(root *cobra.Command, line string, allowHidden bool) string {
	cmd := root
	word := regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	for _, tok := range strings.Fields(line) {
		tok = strings.Trim(tok, "`'\"(),;")
		switch {
		case tok == "" || tok == "&&" || strings.HasPrefix(tok, "<"):
			continue
		case strings.HasPrefix(tok, "-"):
			name := strings.SplitN(strings.TrimLeft(tok, "-"), "=", 2)[0]
			if cmd.Flags().Lookup(name) == nil && cmd.InheritedFlags().Lookup(name) == nil {
				return "unknown flag --" + name + " for `" + cmd.CommandPath() + "`"
			}
			continue
		case !word.MatchString(tok):
			continue // a positional value (an id, a path)
		}
		var next *cobra.Command
		for _, c := range cmd.Commands() {
			if (c.Name() == tok || c.HasAlias(tok)) && (allowHidden || !c.Hidden) {
				next = c
				break
			}
		}
		if next != nil {
			cmd = next
			continue
		}
		if cmd.HasSubCommands() {
			return "`" + cmd.CommandPath() + "` has no subcommand " + tok
		}
	}
	if cmd == root {
		return "names no command"
	}
	// A help-only group is runnable since T-757 (its RunE prints help so the
	// unknown-subcommand check runs), but it still does nothing.
	if !cmd.Runnable() || cmd.Annotations[annotationHelpOnlyGroup] != "" {
		return "`" + cmd.CommandPath() + "` does not run"
	}
	return ""
}

// TestInstallerCommandsExist pins T-746: every observer command install.sh
// and install.ps1 print exists in this binary's command tree with the flags
// they name. rc.2 printed `observer org enroll <link>` and `observer org
// unenroll`, neither of which existed (`observer enroll --link`, `observer
// unenroll`), and the group command printed help and exited 0.
func TestInstallerCommandsExist(t *testing.T) {
	root := newRootCmdWith(usageDeps{})
	for _, name := range []string{"install.sh", "install.ps1"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		// A shell-escaped backtick (\`) inside a double-quoted say is a plain
		// backtick on screen.
		text := strings.ReplaceAll(string(raw), "\\`", "`")
		found := 0
		for _, re := range installerCommandPatterns {
			for _, m := range re.FindAllStringSubmatch(text, -1) {
				// One string may chain commands (`a && b`).
				for _, part := range strings.Split(m[1], "&&") {
					part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "observer "))
					if part == "" || strings.Contains(part, "$") {
						continue // a shell / PowerShell variable, not a literal command
					}
					found++
					if why := resolveInstallerCommand(root, part, false); why != "" {
						t.Errorf("%s prints `observer %s`: %s", name, part, why)
					}
				}
			}
		}
		if found < 3 {
			t.Fatalf("%s: only %d observer commands found; the patterns no longer match the script", name, found)
		}
	}
}

// TestOrgUnknownSubcommandFails pins the other half of T-746: `observer org
// <unknown>` is an error naming the real enrol / leave commands, never help
// and exit 0; the org-prefixed enrol and leave verbs resolve to the real ones.
func TestOrgUnknownSubcommandFails(t *testing.T) {
	root := newRootCmdWith(usageDeps{})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"org", "join-now"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("`observer org join-now` succeeded; output:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "unknown command") || !strings.Contains(err.Error(), "observer enroll --link") {
		t.Fatalf("error %q does not name the unknown command and the real enrol command", err)
	}
	for _, line := range []string{"org enroll --link x", "org unenroll --plan", "enroll --link x", "unenroll"} {
		if why := resolveInstallerCommand(newRootCmdWith(usageDeps{}), line, true); why != "" {
			t.Errorf("`observer %s`: %s", line, why)
		}
	}
}
