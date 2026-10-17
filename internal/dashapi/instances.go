package dashapi

import (
	"errors"
)

// InstanceInfo is one row of the switcher.
//
// It carries exactly what the UI needs to recognise a machine and point a
// browser at it. As with SSHProfileInfo there is no key material and no key
// PATH - KeyHint is a basename - so the switcher discloses nothing the SSH
// picker does not already.
type InstanceInfo struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	// Target is the display-only "user@host:port" summary.
	Target string `json:"target"`
	// DashboardPort is the REMOTE port being forwarded to.
	DashboardPort int    `json:"dashboard_port"`
	HasKey        bool   `json:"has_key"`
	KeyHint       string `json:"key_hint,omitempty"`
	Jump          string `json:"jump,omitempty"`
	// State is "disconnected" | "connecting" | "connected" | "error".
	State string `json:"state"`
	// LocalPort is the loopback port the forward listens on, when live.
	LocalPort int `json:"local_port,omitempty"`
	// URL is the loopback address to open. Present ONLY when the forward
	// actually answers, so the UI never offers a link to a dead port.
	URL string `json:"url,omitempty"`
	// Error is the honest failure reason when State is "error".
	Error string `json:"error,omitempty"`
	// RemoteVersion, RemoteSchemaVersion and RemoteHostName are what the
	// REMOTE daemon reports about itself through the live forward (its own
	// /api/status), so the switcher can say which machine and build answer
	// and flag an older one (T-425, D-325). Present only while connected and
	// only when the remote answered.
	RemoteVersion       string `json:"remote_version,omitempty"`
	RemoteSchemaVersion int    `json:"remote_schema_version,omitempty"`
	RemoteHostName      string `json:"remote_host_name,omitempty"`
	// RemoteProbeError says, naming the machine, why the remote build could
	// not be read. Informational: it never blocks opening the dashboard.
	RemoteProbeError string `json:"remote_probe_error,omitempty"`
}

// InstanceTestResult is the payload of POST /api/instances/{name}/test - the
// dashboard's "Test" affordance next to Connect. It mirrors
// internal/sshforward.TestResult field-for-field; this package does not
// import sshforward (LaunchManager seam discipline, CLAUDE.md #2), so the
// cmd adapter's TestInstance projects one onto the other.
type InstanceTestResult struct {
	// KnownHostsChecked reports whether the known_hosts pre-flight could
	// reach a verdict at all (false when the host alias could not be
	// resolved via `ssh -G`, e.g. ssh itself is missing).
	KnownHostsChecked bool `json:"known_hosts_checked"`
	// KnownHostsOK is the verdict when KnownHostsChecked is true: whether the
	// host is already trusted.
	KnownHostsOK bool `json:"known_hosts_ok"`
	// AuthOK reports whether the bounded probe (BatchMode, ConnectTimeout=5)
	// completed and authenticated successfully.
	AuthOK bool `json:"auth_ok"`
	// LatencyMS is the probe's wall-clock duration in milliseconds.
	LatencyMS int64 `json:"latency_ms"`
	// Stderr is a truncated (~500 byte) tail of the probe's stderr, present
	// only when non-empty - the UI's tooltip on a failed test.
	Stderr string `json:"stderr,omitempty"`
}

// Sentinel errors the cmd adapter maps sshforward's onto, so this package never
// imports the forward manager (LaunchManager seam discipline, CLAUDE.md #2).
var (
	// ErrInstancesDisabled - [terminal.ssh].enabled is off (403).
	ErrInstancesDisabled = errors.New("remote instances are disabled (set [terminal.ssh].enabled)")
	// ErrInstanceUnknown - the named profile is not in the operator's config
	// (400). THE gate: a name nobody wrote can never become a connection.
	ErrInstanceUnknown = errors.New("no such instance profile")
	// ErrInstanceInvalid - the profile failed re-validation at connect time
	// (400), e.g. its key file has been removed since the daemon started.
	ErrInstanceInvalid = errors.New("instance profile is not valid")
	// ErrInstanceHostKeyUnknown - the host is not in known_hosts (409). A -N
	// forward has no terminal on which to accept a new key, and Observer will
	// not auto-accept one; the message names the fix.
	ErrInstanceHostKeyUnknown = errors.New("host key is not in known_hosts")
	// ErrInstanceForwardFailed - ssh started but the forward never came up
	// (502), carrying ssh's own reason.
	ErrInstanceForwardFailed = errors.New("could not open the port forward")
)
