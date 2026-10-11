package sshspec

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrInvalidProfile is the sentinel every validation failure wraps, so callers
// can map the whole class to one HTTP status without matching on message text.
var ErrInvalidProfile = errors.New("sshprofile: invalid profile")

// Field length ceilings (exported so the argv layer and its tests share
// them). They are generous enough for real deployments and
// tight enough that a pathological config can never compose a multi-kilobyte
// argv token.
const (
	MaxName = 64
	MaxHost = 255
	MaxUser = 64
	MaxJump = 320 // [user@]host[:port] with both parts at their own ceilings
	MaxPath = 4096
)

// DefaultSSHPort is the port the -p flag is omitted for: passing "-p 22"
// is harmless but noisy, and omitting it lets a ~/.ssh/config Host block
// supply its own Port for an alias.
const DefaultSSHPort = 22

// Profile is one operator-authored remote system.
//
// Every field originates in the daemon's own config.toml. The dashboard
// supplies only Name (to select a profile); it can never influence Host, User,
// Port, KeyPath, or Jump.
type Profile struct {
	// Name is the stable identifier the API references. Deliberately narrow
	// (lowercase, no separators beyond . _ -) because it travels in a request
	// body and is matched exactly.
	Name string
	// Label is an optional human-readable display name for the picker. It never
	// reaches the argv, so it is validated only for control characters.
	Label string
	// Host is a hostname, an IP literal, or a ~/.ssh/config Host alias. It is
	// the LAST argv token and must never begin with '-'.
	Host string
	// User is an optional login name, passed via -l (never user@host, so an
	// embedded '@' cannot redirect the destination).
	User string
	// Port is an optional TCP port. 0 or 22 omits the -p flag.
	Port int
	// KeyPath is an optional absolute path to a private key file. It is passed
	// to `ssh -i` and is NEVER opened by Observer.
	KeyPath string
	// Jump is an optional [user@]host[:port] ProxyJump spec passed via -J.
	// Multi-hop comma chains are rejected in v1 (plan D3).
	Jump string
	// DashboardPort is the port the REMOTE machine's own Observer dashboard
	// listens on, used by the instance switcher's local port forward
	// (ForwardArgv). 0 means unset and resolves to DefaultDashboardPort.
	//
	// It is the ONLY remote port this feature will ever forward to, and it
	// comes from the operator's config - never from a request. That is the
	// whole reason the forward is not a general tunnel surface: a client can
	// name a profile, and a profile names exactly one port.
	DashboardPort int
	// ReverseProxy opts a profile into an ADDITIONAL -R remote forward on
	// ForwardArgv (the instance-switcher connection) only. Default false: the
	// -L-only, one-directional forward stays the shape for every profile that
	// does not explicitly ask for this. When true, the far end of -R is
	// always THIS machine's own loopback Observer proxy port
	// (DefaultObserverProxyPort) - not configurable per-profile, so widening
	// it can never turn the forward into a pivot into a third port on this
	// machine.
	//
	// Argv (the interactive login, C1) and TestArgv (the diagnostic probe,
	// C2) never honor this field - see their doc comments.
	ReverseProxy bool
	// ReverseProxyPort is the port the -R forward BINDS on the REMOTE side.
	// 0 means unset and resolves to DefaultReverseProxyPort. Meaningful only
	// when ReverseProxy is true.
	ReverseProxyPort int
}

// DefaultDashboardPort is the port a remote Observer dashboard is assumed to
// listen on when a profile does not say otherwise. It matches the daemon's own
// built-in 127.0.0.1:8081 default.
const DefaultDashboardPort = 8081

// ForwardBindAddr is the address BOTH ends of an instance-switcher forward are
// pinned to. It is a constant, not a knob:
//
//   - the LOCAL end must be loopback so the forwarded remote dashboard is not
//     republished to this machine's network;
//   - the REMOTE end must be loopback so the forward reaches the remote
//     daemon's own default bind and can never be aimed at a third machine on
//     the remote's network (which would make this a general-purpose pivot).
const ForwardBindAddr = "127.0.0.1"

// DefaultReverseProxyPort is the REMOTE-side bind port for an opt-in -R
// forward when a profile does not say otherwise. It has no relationship to
// DefaultObserverProxyPort (the fixed LOCAL destination every -R forward
// targets) beyond sharing the same number by convention - an operator running
// more than one profile's reverse forward on the same remote machine sets
// reverse_proxy_port to avoid a collision.
const DefaultReverseProxyPort = 8820

// DefaultObserverProxyPort is the LOCAL port an opt-in -R forward always
// targets: this machine's own Observer proxy. It is a constant, not a
// profile field - the whole point of ReverseProxy is "let the remote machine
// reach my proxy", and letting a profile redirect that to an arbitrary local
// port would turn the forward into a general-purpose pivot into this
// machine, exactly the property ForwardArgv's -L side already refuses.
const DefaultObserverProxyPort = 8820

// ResolvedDashboardPort returns the remote dashboard port for a profile,
// substituting DefaultDashboardPort for an unset (0) field.
func (p Profile) ResolvedDashboardPort() int {
	if p.DashboardPort == 0 {
		return DefaultDashboardPort
	}
	return p.DashboardPort
}

// ResolvedReverseProxyPort returns the remote-side bind port for the opt-in
// -R forward, substituting DefaultReverseProxyPort for an unset (0) field.
func (p Profile) ResolvedReverseProxyPort() int {
	if p.ReverseProxyPort == 0 {
		return DefaultReverseProxyPort
	}
	return p.ReverseProxyPort
}

// Options carries the connection-tuning knobs that are policy, not identity -
// they come from the [terminal.ssh] block rather than from a profile, because
// they are the same for every system.
type Options struct {
	// ConnectTimeoutSeconds bounds the TCP/handshake wait. <= 0 uses
	// DefaultConnectTimeoutSeconds. A dead host must not hold a PTY slot open
	// forever.
	ConnectTimeoutSeconds int
	// KeepaliveSeconds is the ServerAliveInterval. <= 0 uses
	// DefaultKeepaliveSeconds. With ServerAliveCountMax=3 a dead link ends the
	// PTY (and records the run's exit) instead of hanging.
	KeepaliveSeconds int
}

// Defaults for Options' zero value.
const (
	DefaultConnectTimeoutSeconds = 10
	DefaultKeepaliveSeconds      = 30
)

// Resolved fills Options' zero fields with their defaults.
func (o Options) Resolved() Options {
	if o.ConnectTimeoutSeconds <= 0 {
		o.ConnectTimeoutSeconds = DefaultConnectTimeoutSeconds
	}
	if o.KeepaliveSeconds <= 0 {
		o.KeepaliveSeconds = DefaultKeepaliveSeconds
	}
	return o
}

// Display returns the label to show in a picker: the operator's Label when set,
// else the profile Name. Never empty for a validated profile.
func (p Profile) Display() string {
	if strings.TrimSpace(p.Label) != "" {
		return p.Label
	}
	return p.Name
}

// Target renders the "user@host:port" summary line for the picker. It is
// display-only and never reaches an argv.
func (p Profile) Target() string {
	host := p.Host
	if p.Port != 0 && p.Port != DefaultSSHPort {
		host += ":" + strconv.Itoa(p.Port)
	}
	if p.User != "" {
		return p.User + "@" + host
	}
	return host
}

// KeyHint returns the BASENAME of the configured key path, or "" when no key is
// configured. The picker shows this instead of the full path so the dashboard
// never discloses the operator's filesystem layout (plan §6.2).
func (p Profile) KeyHint() string {
	if p.KeyPath == "" {
		return ""
	}
	return filepath.Base(p.KeyPath)
}

// Validate applies every pure syntax rule (plan §9.1). It does not touch the
// filesystem - see ValidateWithFS for the key-path existence check.
func (p Profile) Validate() error {
	if err := validateName(p.Name); err != nil {
		return err
	}
	if err := validateLabel(p.Label); err != nil {
		return err
	}
	if err := validateHost("host", p.Host); err != nil {
		return err
	}
	if p.User != "" {
		if err := validateUser("user", p.User); err != nil {
			return err
		}
	}
	if err := validatePort(p.Port); err != nil {
		return err
	}
	if p.KeyPath != "" {
		if err := validateKeyPath(p.KeyPath); err != nil {
			return err
		}
	}
	if p.Jump != "" {
		if err := validateJump(p.Jump); err != nil {
			return err
		}
	}
	if err := validateDashboardPort(p.DashboardPort); err != nil {
		return err
	}
	if err := validateReverseProxyPort(p.ReverseProxyPort); err != nil {
		return err
	}
	return nil
}

// ValidateWithFS is Validate plus the key-path filesystem check: when a
// KeyPath is configured it must resolve to an EXISTING REGULAR FILE. Observer
// stats it and stops there - the bytes are never read.
//
// It is called at LAUNCH time as well as at config load, mirroring the
// re-validate-immediately-before-spawn discipline termsvc.ValidateProjectRoot
// established: a key file removed after the daemon started must fail the
// launch, not produce a confusing ssh error.
func (p Profile) ValidateWithFS() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.KeyPath == "" {
		return nil
	}
	info, err := os.Stat(p.KeyPath)
	if err != nil {
		return fmt.Errorf("%w: key_path %q: %w", ErrInvalidProfile, p.KeyPath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: key_path %q is not a regular file", ErrInvalidProfile, p.KeyPath)
	}
	return nil
}

// --- field validators -------------------------------------------------------

// validateName enforces the narrow API-key charset for a profile name.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name must not be empty", ErrInvalidProfile)
	}
	if len(name) > MaxName {
		return fmt.Errorf("%w: name %q exceeds %d characters", ErrInvalidProfile, name, MaxName)
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case (r == '.' || r == '_' || r == '-') && i > 0:
		default:
			return fmt.Errorf("%w: name %q must match [a-z0-9][a-z0-9._-]*", ErrInvalidProfile, name)
		}
	}
	return nil
}

// validateLabel rejects control characters in the display label. The label
// never reaches an argv, so nothing stricter is warranted - but a label with an
// embedded escape sequence could garble the dashboard, so control bytes go.
func validateLabel(label string) error {
	if len(label) > MaxHost {
		return fmt.Errorf("%w: label exceeds %d characters", ErrInvalidProfile, MaxHost)
	}
	for _, r := range label {
		if r < ' ' || r == 0x7f {
			return fmt.Errorf("%w: label contains a control character", ErrInvalidProfile)
		}
	}
	return nil
}

// validateHost accepts a hostname / IPv4 / ~/.ssh/config Host alias matching
// [A-Za-z0-9][A-Za-z0-9._-]*, OR an IPv6 literal (bare or bracketed). Anything
// else is refused.
//
// The leading-'-' rejection is load-bearing: ssh has no "--" separator, so a
// host beginning with '-' would be parsed as a flag.
func validateHost(field, host string) error {
	if host == "" {
		return fmt.Errorf("%w: %s must not be empty", ErrInvalidProfile, field)
	}
	if len(host) > MaxHost {
		return fmt.Errorf("%w: %s exceeds %d characters", ErrInvalidProfile, field, MaxHost)
	}
	if err := noControlOrSpace(field, host); err != nil {
		return err
	}
	if strings.HasPrefix(host, "-") {
		return fmt.Errorf("%w: %s %q must not begin with '-'", ErrInvalidProfile, field, host)
	}
	// IPv6 literal, bracketed or bare. Checked before the charset rule because
	// ':' is not in it.
	if strings.Contains(host, ":") {
		bare := strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		if ip := net.ParseIP(bare); ip != nil && ip.To4() == nil {
			return nil
		}
		return fmt.Errorf("%w: %s %q is not a valid IPv6 literal", ErrInvalidProfile, field, host)
	}
	for i, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case (r == '.' || r == '_' || r == '-') && i > 0:
		default:
			return fmt.Errorf("%w: %s %q contains a character outside [A-Za-z0-9._-]", ErrInvalidProfile, field, host)
		}
	}
	return nil
}

// validateUser enforces the login-name charset. Leading '-' is rejected for the
// same flag-injection reason as the host.
func validateUser(field, user string) error {
	if len(user) > MaxUser {
		return fmt.Errorf("%w: %s exceeds %d characters", ErrInvalidProfile, field, MaxUser)
	}
	if err := noControlOrSpace(field, user); err != nil {
		return err
	}
	for i, r := range user {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_':
		case r == '-' && i > 0:
		default:
			return fmt.Errorf("%w: %s %q contains a character outside [A-Za-z0-9._-] (or begins with '-')", ErrInvalidProfile, field, user)
		}
	}
	return nil
}

// validatePort bounds the TCP port. 0 means "unset" and omits the flag.
func validatePort(port int) error {
	if port == 0 {
		return nil
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: port %d is outside 1..65535", ErrInvalidProfile, port)
	}
	return nil
}

// validateDashboardPort bounds the remote Observer dashboard port. Like
// Profile.Port, 0 means "unset" - but unlike Port it resolves to a REAL default
// (DefaultDashboardPort) rather than omitting a flag, because a forward with no
// destination port is not expressible.
func validateDashboardPort(port int) error {
	if port == 0 {
		return nil
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: dashboard_port %d is outside 1..65535", ErrInvalidProfile, port)
	}
	return nil
}

// validateReverseProxyPort bounds the opt-in -R forward's remote-side bind
// port. Like validateDashboardPort, 0 means "unset" and resolves to a real
// default (DefaultReverseProxyPort) rather than omitting anything.
func validateReverseProxyPort(port int) error {
	if port == 0 {
		return nil
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: reverse_proxy_port %d is outside 1..65535", ErrInvalidProfile, port)
	}
	return nil
}

// ValidateForwardPort bounds a port that actually reaches the -L argument. It
// is STRICTER than validatePort: 0 is refused, because at argv-composition time
// every port has already been resolved and a 0 here would mean "let the OS
// pick", which OpenSSH does not report back to us - leaving the UI unable to
// say honestly which port answers.
func ValidateForwardPort(field string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: %s %d is outside 1..65535", ErrInvalidProfile, field, port)
	}
	return nil
}

// validateKeyPath enforces the PATH rules. It never opens the file - see
// ValidateWithFS for the stat. UNC/device prefixes are refused with the same
// rule set termsvc.rejectDangerousPath applies to project roots.
func validateKeyPath(path string) error {
	if len(path) > MaxPath {
		return fmt.Errorf("%w: key_path exceeds %d characters", ErrInvalidProfile, MaxPath)
	}
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("%w: key_path contains a NUL byte", ErrInvalidProfile)
	}
	for _, r := range path {
		if r < ' ' || r == 0x7f {
			return fmt.Errorf("%w: key_path contains a control character", ErrInvalidProfile)
		}
	}
	if strings.HasPrefix(path, "-") {
		return fmt.Errorf("%w: key_path %q must not begin with '-'", ErrInvalidProfile, path)
	}
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		return fmt.Errorf("%w: key_path %q must not be a UNC/network path", ErrInvalidProfile, path)
	}
	if strings.Contains(path, `\\.\`) || strings.Contains(path, `\\?\`) {
		return fmt.Errorf("%w: key_path %q must not be a device-namespace path", ErrInvalidProfile, path)
	}
	// Absolute is required against the DAEMON's own OS. filepath.IsAbs is
	// host-only, which is exactly what we want: the key is read by the local
	// ssh client on this machine.
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: key_path %q must be an absolute path", ErrInvalidProfile, path)
	}
	return nil
}

// validateJump validates a [user@]host[:port] ProxyJump spec, applying the same
// per-component rules as the primary destination.
//
// Comma-separated multi-hop chains are refused in v1 (plan D3) - the validator
// loop is trivial to add later, but every hop is another destination the
// operator has to have thought about.
func validateJump(jump string) error {
	if len(jump) > MaxJump {
		return fmt.Errorf("%w: jump exceeds %d characters", ErrInvalidProfile, MaxJump)
	}
	if err := noControlOrSpace("jump", jump); err != nil {
		return err
	}
	if strings.Contains(jump, ",") {
		return fmt.Errorf("%w: jump %q must be a single hop (multi-hop chains are not supported)", ErrInvalidProfile, jump)
	}
	rest := jump
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		user := rest[:at]
		if user == "" {
			return fmt.Errorf("%w: jump %q has an empty user", ErrInvalidProfile, jump)
		}
		if err := validateUser("jump user", user); err != nil {
			return err
		}
		rest = rest[at+1:]
	}
	// Split an optional :port off the host part. Three shapes, in the order
	// they must be distinguished: a BRACKETED IPv6 literal (the only IPv6 form
	// that may carry a port), a BARE IPv6 literal (whose colons are address
	// bytes and must never be split), and everything else (hostname / IPv4).
	switch {
	case strings.HasPrefix(rest, "["):
		end := strings.Index(rest, "]")
		if end < 0 {
			return fmt.Errorf("%w: jump %q has an unclosed '['", ErrInvalidProfile, jump)
		}
		if tail := rest[end+1:]; tail != "" {
			if !strings.HasPrefix(tail, ":") {
				return fmt.Errorf("%w: jump %q has trailing text after ']'", ErrInvalidProfile, jump)
			}
			if err := validateJumpPort(jump, tail[1:]); err != nil {
				return err
			}
		}
		rest = rest[:end+1]
	case isBareIPv6(rest):
		// No port suffix is expressible without brackets - leave rest intact.
	default:
		if idx := strings.LastIndex(rest, ":"); idx >= 0 {
			if err := validateJumpPort(jump, rest[idx+1:]); err != nil {
				return err
			}
			rest = rest[:idx]
		}
	}
	return validateHost("jump host", rest)
}

// validateJumpPort parses and bounds the :port suffix of a jump spec. Port 0 is
// rejected here (unlike a profile's own Port field, where 0 legitimately means
// "unset"): an explicitly written ":0" is a malformed spec, not an omission.
func validateJumpPort(jump, portStr string) error {
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("%w: jump %q has a non-numeric port", ErrInvalidProfile, jump)
	}
	if port == 0 {
		return fmt.Errorf("%w: jump %q has port 0", ErrInvalidProfile, jump)
	}
	return validatePort(port)
}

// isBareIPv6 reports whether s is an unbracketed IPv6 literal, which must not
// be split on ':' when looking for a port suffix.
func isBareIPv6(s string) bool {
	if strings.HasPrefix(s, "[") {
		return false
	}
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() == nil
}

// noControlOrSpace rejects NUL, whitespace, and other control characters - the
// same class internal/workspace and internal/integration reject for an argv
// token.
func noControlOrSpace(field, s string) error {
	for _, r := range s {
		if r <= ' ' || r == 0x7f {
			return fmt.Errorf("%w: %s %q contains whitespace or a control character", ErrInvalidProfile, field, s)
		}
	}
	return nil
}
