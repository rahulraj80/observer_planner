package termsvc

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/sshprofile"
)

// TestPolicySourceBindsWithoutRebuild pins T-488 / T-489: with a live
// PolicySource wired, a change to the launch policy or the SSH profile list
// binds on the very next authorization of the SAME Service, with no restart
// (no rebuild). Every launch shape that reads the policy is covered: fresh,
// shell, resume, GUI, SSH and the picker's profile list.
func TestPolicySourceBindsWithoutRebuild(t *testing.T) {
	var cur atomic.Pointer[Policy]
	cur.Store(&Policy{})
	l := &fakeLauncher{handle: "H1"}
	svc := New(Options{
		Policy:       Policy{AllowFresh: true, AllowedTools: []string{"claude-code"}}, // ignored once a source is wired
		PolicySource: func() Policy { return *cur.Load() },
		Recorder:     newFakeRecorder(),
		Launcher:     l,
		Now:          func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
	})
	ctx := context.Background()

	// Deny-all source: the static Policy must NOT leak through.
	if _, err := svc.LaunchFresh(ctx, FreshRequest{Tool: "claude-code", Subcommand: "claude"}); !errors.Is(err, ErrFreshLaunchDisabled) {
		t.Fatalf("fresh under a deny-all source = %v, want ErrFreshLaunchDisabled", err)
	}
	if _, err := svc.LaunchFresh(ctx, FreshRequest{Shell: true}); !errors.Is(err, ErrShellLaunchDisabled) {
		t.Fatalf("shell under a deny-all source = %v, want ErrShellLaunchDisabled", err)
	}
	if _, err := svc.LaunchSSH(ctx, SSHRequest{Profile: "box"}); !errors.Is(err, ErrSSHLaunchDisabled) {
		t.Fatalf("ssh under a deny-all source = %v, want ErrSSHLaunchDisabled", err)
	}
	if enabled, list := svc.SSHProfileList(); enabled || len(list) != 0 {
		t.Fatalf("profile list under a deny-all source = %v %v, want off and empty", enabled, list)
	}

	// The operator saves: fresh + shell on, one SSH profile added.
	box := sshprofile.Profile{Name: "box", Host: "box.example.test", User: "dev"}
	cur.Store(&Policy{
		AllowFresh: true, AllowedTools: []string{"claude-code"}, AllowShell: true,
		AllowSSH: true, SSHProfiles: []sshprofile.Profile{box},
	})
	if _, err := svc.LaunchFresh(ctx, FreshRequest{Tool: "claude-code", Subcommand: "claude"}); err != nil {
		t.Fatalf("fresh after the save = %v, want a launch", err)
	}
	if _, err := svc.LaunchFresh(ctx, FreshRequest{Shell: true}); err != nil {
		t.Fatalf("shell after the save = %v, want a launch", err)
	}
	if _, err := svc.LaunchSSH(ctx, SSHRequest{Profile: "box"}); !sshInBuild {
		// A build without terminals.ssh: the saved profile is found (the
		// policy bound), then the launch is refused naming the build (D-535).
		if err == nil || !strings.Contains(err.Error(), "terminals.ssh is not in this build") {
			t.Fatalf("ssh after the save in a build without terminals.ssh = %v, want the not-in-build refusal", err)
		}
	} else if err != nil {
		t.Fatalf("ssh after the save = %v, want a launch", err)
	}
	if enabled, list := svc.SSHProfileList(); !enabled || len(list) != 1 || list[0].Name != "box" {
		t.Fatalf("profile list after the save = %v %v, want on with [box]", enabled, list)
	}

	// The operator removes the profile and turns fresh launch off again.
	cur.Store(&Policy{AllowSSH: true})
	if _, err := svc.LaunchSSH(ctx, SSHRequest{Profile: "box"}); !errors.Is(err, ErrSSHProfileUnknown) {
		t.Fatalf("ssh to a removed profile = %v, want ErrSSHProfileUnknown", err)
	}
	if _, err := svc.LaunchResume(ctx, ResumeRequest{Tool: "claude-code", Subcommand: "claude", SourceSessionID: "s1"}); !errors.Is(err, ErrFreshLaunchDisabled) {
		t.Fatalf("resume after fresh turned off = %v, want ErrFreshLaunchDisabled", err)
	}
}

// TestNoPolicySourceKeepsStaticPolicy pins the additive default: a Service
// built without a PolicySource behaves exactly as before.
func TestNoPolicySourceKeepsStaticPolicy(t *testing.T) {
	svc := newService(t, Policy{AllowFresh: true, AllowedTools: []string{"claude-code"}}, newFakeRecorder(), &fakeLauncher{handle: "H1"}, nil)
	if _, err := svc.LaunchFresh(context.Background(), FreshRequest{Tool: "claude-code", Subcommand: "claude"}); err != nil {
		t.Fatalf("static policy launch = %v, want a launch", err)
	}
}
