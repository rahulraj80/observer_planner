package policy

import (
	"strings"
	"testing"
	"time"
)

// TestSensitivePathRulesFollowTheCommandsDirectory is T-387 (the D-279
// follow-up): the sensitive-path rows (R-152, R-154, R-155, R-156, R-160,
// R-161, shell and inline-script alike) judge a relative operand from
// every directory the unit can run in and through links the same line
// created, the cwdtrack.go model R-101/R-102 already use, instead of from
// the event's own directory. Fixture: home /home/u, project and cwd
// /home/u/proj (Windows rows: home C:\Users\u, project C:\proj).
//
// The cases deliberately move to a NON-sensitive parent first: a `cd`
// straight into a sensitive directory is already a read of it and was
// always flagged on the `cd` unit itself.
func TestSensitivePathRulesFollowTheCommandsDirectory(t *testing.T) {
	t.Parallel()
	const winHome, winRoot = `C:\Users\u`, `C:\proj`
	cases := []destructiveCase{
		// --- R-152 sensitive credential locations ----------------------
		{name: "R-152 read after cd home", cmd: "cd ~ && cat .aws/config", wantRule: "R-152", wantEnforce: DecisionAsk},
		{name: "R-152 write after cd home", cmd: "cd ~ && echo x > .aws/config", wantRule: "R-152", wantEnforce: DecisionDeny},
		{name: "R-152 read through pushd", cmd: "pushd ~; cat .aws/config", wantRule: "R-152", wantEnforce: DecisionAsk},
		{name: "R-152 read inside a subshell", cmd: "(cd ~ && cat .aws/config)", wantRule: "R-152", wantEnforce: DecisionAsk},
		{name: "R-152 env chdir wrapper", cmd: "env -C ~ cat .aws/config", wantRule: "R-152", wantEnforce: DecisionAsk},
		{name: "R-152 nested shell inherits the cd", cmd: "cd ~ && bash -c 'cat .aws/config'", wantRule: "R-152", wantEnforce: DecisionAsk},
		{name: "R-152 inline script after cd", cmd: `cd ~ && python3 -c "print(open('.aws/config').read())"`, wantRule: "R-152", wantEnforce: DecisionAsk},
		{name: "R-152 powershell Set-Location", cmd: `Set-Location ~; Get-Content .aws\config`, dialect: DialectPowerShell, home: winHome, root: winRoot, wantRule: "R-152", wantEnforce: DecisionAsk},
		{name: "R-152 cmd cd /d", cmd: `cd /d C:\Users\u & type .aws\config`, dialect: DialectCmd, home: winHome, root: winRoot, wantRule: "R-152", wantEnforce: DecisionAsk},

		// --- R-154 / R-155 / R-156 / R-160 ------------------------------
		{name: "R-154 append to bashrc after cd home", cmd: "cd ~ && echo 'alias x=y' >> .bashrc", wantRule: "R-154", wantEnforce: DecisionDeny},
		{name: "R-154 tee a bare profile name after cd home", cmd: "cd ~ && tee .bashrc", wantRule: "R-154", wantEnforce: DecisionDeny},
		{name: "R-154 through a same-command link", cmd: "ln -s ~ h && echo x >> h/.bashrc", wantRule: "R-154", wantEnforce: DecisionDeny},
		{name: "R-154 inline delete after cd home", cmd: `cd ~ && python3 -c "import os; os.remove('.bashrc')"`, wantRule: "R-154", wantEnforce: DecisionDeny},
		{name: "R-155 tee into the systemd user dir", cmd: "cd ~/.config/systemd/user && tee evil.service", wantRule: "R-155", wantEnforce: DecisionDeny},
		{name: "R-156 tee into git hooks", cmd: "cd .git/hooks && tee pre-commit", wantRule: "R-156", wantEnforce: DecisionAsk},
		{name: "R-160 observer config after cd", cmd: "cd ~/.observer && tee config.toml", wantRule: "R-160", wantEnforce: DecisionDeny},
		{name: "R-160 not R-161 for the user-level policy file", cmd: "cd ~ && echo x > .observer/guard-policy.toml", wantRule: "R-160", wantEnforce: DecisionDeny},

		// --- what the model must not lose ------------------------------
		{name: "unknown directory keeps the event-directory reading", cmd: "cd $X && cat .aws/config", cwd: "/home/u", wantRule: "R-152", wantEnforce: DecisionAsk},
		{name: "semicolon failure branch keeps the event directory", cmd: "cd /home/u/proj; echo x >> .bashrc", cwd: "/home/u", wantRule: "R-154", wantEnforce: DecisionDeny},
		{name: "absolute operand is unchanged", cmd: "cd /tmp && cat ~/.aws/config", wantRule: "R-152", wantEnforce: DecisionAsk},
		// The event-directory reading resolved ../ from the project root
		// into ~/.observer and reported R-160 for a project file.
		{name: "R-161 not R-160 after cd into a subdirectory", cmd: "cd web && echo x > ../.observer/guard-policy.toml", wantRule: "R-161", wantEnforce: DecisionFlag},

		// --- benign controls -------------------------------------------
		{name: "ok: cd into the project then write .bashrc there", cmd: "cd /home/u/proj && echo x >> .bashrc", cwd: "/home/u"},
		{name: "ok: cd web then read config", cmd: "cd web && cat config.json"},
		{name: "ok: cd tmp then write a profile-named file", cmd: "cd /tmp && echo x > .bashrc"},
		{name: "ok: unknown directory, nothing sensitive from the event directory", cmd: "cd $X && cat config"},
		{name: "ok: link inside the project", cmd: "ln -s dist out && cat out/index.html"},
		{name: "ok: bare words after cd home", cmd: "cd ~ && git status"},
		{name: "ok: bare words after cd into a non-sensitive config dir", cmd: "cd ~/.config && echo hello"},
		{name: "ok: bare tee in the event directory", cmd: "tee notes.txt"},
	}
	runRuleCases(t, cases, KindShellExec, "run_command")
}

// TestSensitivePathReasonNamesTheMove pins that a hit judged from a moved
// directory says so.
func TestSensitivePathReasonNamesTheMove(t *testing.T) {
	t.Parallel()
	eng, err := New(Config{Mode: ModeEnforce, Home: "/home/u"})
	if err != nil {
		t.Fatal(err)
	}
	v := eng.Evaluate(Event{
		Kind: KindShellExec, ActionType: "run_command", Target: "cd ~ && cat .aws/config",
		Cwd: "/home/u/proj", ProjectRoot: "/home/u/proj", Now: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
	})
	if v.RuleID != "R-152" || !strings.Contains(v.Reason, "earlier step of this command") {
		t.Fatalf("verdict = %s %q, want R-152 naming the earlier step", v.RuleID, v.Reason)
	}
}

// TestCopyDestinationAndBareWriteInTheEventDirectory is T-526, the two
// residuals GUARD-PATH-CWD-1 recorded: (1) cp / mv took the last PATH-LIKE
// operand for the destination, so a bare destination name after a `cd`
// (`cd ~/.config/autostart && cp /tmp/x.desktop evil.desktop`) was the
// source read, not a write; (2) a bare sensitive name WRITTEN from the
// event's own directory (`tee .bashrc` typed in home) was never judged.
// Fixture: home /home/u, project /home/u/proj, cwd per row.
func TestCopyDestinationAndBareWriteInTheEventDirectory(t *testing.T) {
	t.Parallel()
	cases := []destructiveCase{
		// --- (1) copy / move destination --------------------------------
		{name: "R-155 cp bare destination after cd autostart", cmd: "cd ~/.config/autostart && cp /tmp/x.desktop evil.desktop", wantRule: "R-155", wantEnforce: DecisionDeny},
		{name: "R-155 mv bare destination after cd autostart", cmd: "cd ~/.config/autostart && mv /tmp/x.desktop evil.desktop", wantRule: "R-155", wantEnforce: DecisionDeny},
		{name: "R-154 cp a bare profile name after cd home", cmd: "cd ~ && cp /tmp/rc .bashrc", wantRule: "R-154", wantEnforce: DecisionDeny},
		{name: "R-156 cp into git hooks", cmd: "cd .git/hooks && cp /tmp/h pre-commit", wantRule: "R-156", wantEnforce: DecisionAsk},
		{name: "R-160 cp observer config after cd", cmd: "cd ~/.observer && cp /tmp/c config.toml", wantRule: "R-160", wantEnforce: DecisionDeny},
		{name: "R-155 cp with flags before the operands", cmd: "cd ~/.config/autostart && cp -f -p /tmp/x.desktop evil.desktop", wantRule: "R-155", wantEnforce: DecisionDeny},
		// --- (2) bare write typed in the event's own directory ----------
		{name: "R-154 tee bashrc typed in home", cmd: "tee .bashrc", cwd: "/home/u", wantRule: "R-154", wantEnforce: DecisionDeny},
		{name: "R-154 cp to bashrc typed in home", cmd: "cp /tmp/rc .bashrc", cwd: "/home/u", wantRule: "R-154", wantEnforce: DecisionDeny},
		{name: "R-155 tee typed in autostart", cmd: "tee evil.desktop", cwd: "/home/u/.config/autostart", wantRule: "R-155", wantEnforce: DecisionDeny},
		{name: "R-156 tee typed in git hooks", cmd: "tee pre-commit", cwd: "/home/u/proj/.git/hooks", wantRule: "R-156", wantEnforce: DecisionAsk},
		{name: "R-160 tee typed in observer dir", cmd: "tee config.toml", cwd: "/home/u/.observer", wantRule: "R-160", wantEnforce: DecisionDeny},
		{name: "R-161 tee project policy typed in its dir", cmd: "tee guard-policy.toml", cwd: "/home/u/proj/.observer", wantRule: "R-161", wantEnforce: DecisionFlag},
		// --- benign controls -------------------------------------------
		{name: "ok: cp a bare destination in the project", cmd: "cp /tmp/x.desktop evil.desktop"},
		{name: "ok: cp a profile-named file within the project", cmd: "cp /tmp/rc .bashrc"},
		{name: "ok: tee a profile-named file within the project", cmd: "tee .bashrc"},
		{name: "ok: cp from home into the project", cmd: "cp .bashrc backup.txt", cwd: "/home/u"},
		{name: "ok: bare read in home stays unjudged", cmd: "grep config notes.txt", cwd: "/home/u"},
		{name: "ok: cd tmp then cp to a profile-named file", cmd: "cd /tmp && cp /home/u/proj/a .bashrc"},
	}
	runRuleCases(t, cases, KindShellExec, "run_command")
}
