package sshprofile

import (
	"strconv"

	"github.com/marmutapp/superbased-observer/internal/sshspec"
)

// The profile model, its validation and the connection-option defaults live
// in internal/sshspec, a neutral core package, so internal/config can
// validate [[terminal.ssh.profiles]] without importing this feature package
// (modular packaging phase PB, rev F1). These aliases keep every existing
// caller compiling unchanged.
type (
	// Profile is sshspec.Profile.
	Profile = sshspec.Profile
	// Options is sshspec.Options.
	Options = sshspec.Options
)

// ErrInvalidProfile is the sentinel every validation failure wraps.
var ErrInvalidProfile = sshspec.ErrInvalidProfile

// Re-exported constants (see internal/sshspec for their documentation).
const (
	DefaultDashboardPort         = sshspec.DefaultDashboardPort
	ForwardBindAddr              = sshspec.ForwardBindAddr
	DefaultReverseProxyPort      = sshspec.DefaultReverseProxyPort
	DefaultObserverProxyPort     = sshspec.DefaultObserverProxyPort
	DefaultConnectTimeoutSeconds = sshspec.DefaultConnectTimeoutSeconds
	DefaultKeepaliveSeconds      = sshspec.DefaultKeepaliveSeconds
)

// serverAliveCountMax is fixed rather than configurable: with the keepalive
// interval already exposed, a second knob only adds ways to configure a link
// that never dies.
const serverAliveCountMax = 3

// Find looks a profile up by exact name. It is the ONLY way a client-supplied
// string turns into a Profile: an unknown name yields ok=false and the caller
// fails the launch, so a name that is not already in the operator's config can
// never reach an argv.
func Find(profiles []Profile, name string) (Profile, bool) {
	for _, p := range profiles {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

// Argv composes the OpenSSH client argv for a profile (plan §4.1). It
// re-validates first, so there is no way to reach the argv builder with an
// unvalidated profile.
//
// The composed command carries NO remote command, which is what yields an
// interactive login shell — and means there is no remote command string for
// anything to be injected into.
//
// The -o options are set EXPLICITLY rather than left to defaults because
// OpenSSH takes the first obtained value for each option and consults the
// command line before ~/.ssh/config: an operator with a global
// `StrictHostKeyChecking no` in their config still gets `ask` here.
func Argv(p Profile, o Options) ([]string, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	o = o.Resolved()

	argv := []string{
		"ssh",
		// Force remote PTY allocation. Our stdin IS a PTY so -t would suffice;
		// -tt is the unconditional form.
		"-tt",
		// Deliberately NOT BatchMode=yes: suppressing prompts would turn a
		// first connection (unknown host key) and an agent-less passphrase
		// into opaque failures instead of questions the operator — who is
		// looking straight at this terminal — can answer.
		"-o", "BatchMode=no",
		// NEVER accept-new, NEVER no, and NOT configurable. Auto-accepting a
		// host key from a daemon-driven launch would silently defeat MITM
		// protection for a machine the operator may never have reached before.
		// known_hosts is left at the operator's own default file.
		"-o", "StrictHostKeyChecking=ask",
		"-o", "ConnectTimeout=" + strconv.Itoa(o.ConnectTimeoutSeconds),
		"-o", "ServerAliveInterval=" + strconv.Itoa(o.KeepaliveSeconds),
		"-o", "ServerAliveCountMax=" + strconv.Itoa(serverAliveCountMax),
	}
	if p.KeyPath != "" {
		// IdentitiesOnly keeps ssh from offering every agent identity ahead of
		// the configured key and hitting "Too many authentication failures".
		// An agent-held passphrase for THIS key still works: ssh matches the
		// agent entry by the public key beside the path.
		argv = append(argv, "-o", "IdentitiesOnly=yes", "-i", p.KeyPath)
	}
	if p.Port != 0 && p.Port != sshspec.DefaultSSHPort {
		argv = append(argv, "-p", strconv.Itoa(p.Port))
	}
	if p.Jump != "" {
		argv = append(argv, "-J", p.Jump)
	}
	if p.User != "" {
		// -l rather than user@host: an '@' inside the user field then cannot
		// redirect the destination host.
		argv = append(argv, "-l", p.User)
	}
	// Host is LAST and carries no remote command after it. ssh has no "--", so
	// validateHost's leading-'-' rejection is what makes this position safe.
	argv = append(argv, p.Host)
	return argv, nil
}

// ForwardArgv composes the OpenSSH client argv for an INSTANCE-SWITCHER local
// port forward: a command-less `ssh -N -L 127.0.0.1:<local>:127.0.0.1:<remote>`
// that makes the profile's remote Observer dashboard reachable on this
// machine's loopback (docs/plans/ssh-remote-profiles-plan-2026-08-27.md §12 D2,
// partially lifted for -L to the dashboard port only).
//
// It shares every hardening rule with Argv and weakens NONE of them. The three
// deliberate differences, each of which makes the command STRICTER:
//
//  1. -N — no remote command AND no remote shell. The child exists solely to
//     carry the forward, so there is no PTY, nothing to type into, and no
//     remote command string for anything to be injected into.
//  2. BatchMode=yes instead of no. Argv leaves prompts ON because the operator
//     is looking straight at that PTY and can answer an unknown-host question.
//     A -N child has no terminal and nobody to ask, so prompting would hang;
//     BatchMode=yes fails fast instead. Combined with the unchanged
//     StrictHostKeyChecking=ask, an unknown host is REFUSED, never
//     auto-accepted — which is why the caller must require the host to be in
//     known_hosts already and say so honestly.
//  3. ExitOnForwardFailure=yes — if the local bind loses its race, the child
//     must die rather than sit there as a live connection carrying no forward
//     (which would leave the UI claiming a port that answers nothing).
//
// The port arguments are deliberately asymmetric about where they come from:
// localPort is allocated by the DAEMON (never supplied by a client), and the
// remote port is read off the PROFILE, so the only thing a request can choose
// is WHICH operator-authored profile to reach. There is no code path here that
// can forward to a port a client named.
//
// -D (SOCKS proxy) and -w (tunnel device) are never composed, under any
// profile. -R (remote forward) is likewise never composed by DEFAULT — this
// stays a one-directional, one-port, loopback-to-loopback forward for every
// profile that leaves ReverseProxy unset — but a profile may opt in with
// ReverseProxy=true (C3), which appends EXACTLY ONE additional -R, itself
// loopback-bound on both ends and pointed at a fixed local destination
// (DefaultObserverProxyPort): see the ReverseProxy field doc for why the
// destination is not a knob. Argv (the interactive login) and TestArgv (the
// diagnostic probe) never honor ReverseProxy and never emit -R.
func ForwardArgv(p Profile, o Options, localPort int) ([]string, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if err := sshspec.ValidateForwardPort("local forward port", localPort); err != nil {
		return nil, err
	}
	remotePort := p.ResolvedDashboardPort()
	if err := sshspec.ValidateForwardPort("dashboard_port", remotePort); err != nil {
		return nil, err
	}
	o = o.Resolved()

	argv := []string{
		"ssh",
		// No remote command, no remote shell — the child only carries -L.
		"-N",
		// See the (2) note above: strictly fewer interactive escapes than Argv.
		"-o", "BatchMode=yes",
		// IDENTICAL to Argv. NEVER accept-new, NEVER no, not configurable.
		"-o", "StrictHostKeyChecking=ask",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ConnectTimeout=" + strconv.Itoa(o.ConnectTimeoutSeconds),
		"-o", "ServerAliveInterval=" + strconv.Itoa(o.KeepaliveSeconds),
		"-o", "ServerAliveCountMax=" + strconv.Itoa(serverAliveCountMax),
	}
	if p.KeyPath != "" {
		argv = append(argv, "-o", "IdentitiesOnly=yes", "-i", p.KeyPath)
	}
	if p.Port != 0 && p.Port != sshspec.DefaultSSHPort {
		argv = append(argv, "-p", strconv.Itoa(p.Port))
	}
	if p.Jump != "" {
		argv = append(argv, "-J", p.Jump)
	}
	if p.User != "" {
		argv = append(argv, "-l", p.User)
	}
	// Both ends pinned to loopback — see ForwardBindAddr. The token begins with
	// a digit, so it can never be read as a flag.
	argv = append(argv, "-L", ForwardArg(localPort, remotePort))
	if p.ReverseProxy {
		reversePort := p.ResolvedReverseProxyPort()
		if err := sshspec.ValidateForwardPort("reverse_proxy_port", reversePort); err != nil {
			return nil, err
		}
		// Exactly one -R, loopback-bound both ends (ForwardArg renders the same
		// bind:port:bind:port shape -R expects), remote side fixed at
		// DefaultObserverProxyPort — never a profile-chosen destination.
		argv = append(argv, "-R", ForwardArg(reversePort, DefaultObserverProxyPort))
	}
	// Host LAST, and with -N there is nothing after it at all.
	argv = append(argv, p.Host)
	return argv, nil
}

// ForwardArg renders the -L argument for a loopback-to-loopback forward. It is
// exported so tests (and callers logging the forward) render it exactly one
// way; the bind addresses are constants, never parameters.
func ForwardArg(localPort, remotePort int) string {
	return ForwardBindAddr + ":" + strconv.Itoa(localPort) +
		":" + ForwardBindAddr + ":" + strconv.Itoa(remotePort)
}

// TestConnectTimeoutSeconds is the FIXED connect timeout for TestArgv's
// one-shot probe (C2). It intentionally ignores Options.ConnectTimeoutSeconds
// — a "test connection" click is a cheap, bounded diagnostic, not a real
// session, and must return quickly even when the operator has configured a
// generous connect timeout for real logins.
const TestConnectTimeoutSeconds = 5

// TestArgv composes the OpenSSH client argv for the diagnostic "test
// connection" probe (C2): a bounded, non-interactive `ssh ... exit` that
// authenticates and immediately disconnects, so the dashboard and the CLI can
// report whether a profile actually connects without opening a shell or a
// forward.
//
// It shares Argv and ForwardArgv's hardening and adds nothing new to the
// attack surface:
//
//   - BatchMode=yes, like ForwardArgv — a probe has no terminal to prompt at,
//     so an unknown host key or a passphrase-less agent failure must fail
//     fast rather than hang.
//   - ConnectTimeout is FIXED at TestConnectTimeoutSeconds, not read from
//     Options — see that constant's doc.
//   - The remote command is the single literal token "exit", never anything
//     derived from the profile or a request.
//
// -L, -R, -D and -w are NEVER composed here, regardless of the profile's
// ReverseProxy setting: a connectivity probe forwards nothing.
func TestArgv(p Profile, o Options) ([]string, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	o = o.Resolved()

	argv := []string{
		"ssh",
		// See the BatchMode note above.
		"-o", "BatchMode=yes",
		// IDENTICAL to Argv and ForwardArgv. NEVER accept-new, NEVER no.
		"-o", "StrictHostKeyChecking=ask",
		"-o", "ConnectTimeout=" + strconv.Itoa(TestConnectTimeoutSeconds),
		"-o", "ServerAliveInterval=" + strconv.Itoa(o.KeepaliveSeconds),
		"-o", "ServerAliveCountMax=" + strconv.Itoa(serverAliveCountMax),
	}
	if p.KeyPath != "" {
		argv = append(argv, "-o", "IdentitiesOnly=yes", "-i", p.KeyPath)
	}
	if p.Port != 0 && p.Port != sshspec.DefaultSSHPort {
		argv = append(argv, "-p", strconv.Itoa(p.Port))
	}
	if p.Jump != "" {
		argv = append(argv, "-J", p.Jump)
	}
	if p.User != "" {
		argv = append(argv, "-l", p.User)
	}
	// Host, then the single literal remote command "exit" — never anything
	// derived from the profile or a request.
	argv = append(argv, p.Host, "exit")
	return argv, nil
}

// ResolveArgv composes `ssh -G`, which parses the operator's ~/.ssh/config for
// a destination and prints the EFFECTIVE settings without connecting to
// anything. Callers use it to turn a Host alias into the real hostname/port
// before asking known_hosts about it, so an alias is not misreported as an
// unknown host.
//
// It connects to nothing and runs no remote command, which is why it is safe to
// run as a pre-flight. BatchMode=yes is set so it can never block on a prompt.
func ResolveArgv(p Profile) ([]string, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	argv := []string{"ssh", "-G", "-o", "BatchMode=yes"}
	if p.Port != 0 && p.Port != sshspec.DefaultSSHPort {
		argv = append(argv, "-p", strconv.Itoa(p.Port))
	}
	if p.User != "" {
		argv = append(argv, "-l", p.User)
	}
	argv = append(argv, p.Host)
	return argv, nil
}
