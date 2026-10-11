package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// terminal_ssh_profiles.go is the dashboard editor for the SSH remote systems
// (T-488): [terminal.ssh].enabled and the [[terminal.ssh.profiles]] list the
// New Terminal "System" picker and the top-bar instance switcher resolve
// against. Before it, both told the operator to hand-edit config.toml and
// restart the daemon.
//
// AUTHORITY: the profile list IS the authorization model for SSH terminals
// (a name nobody wrote can never become a connection). This editor does not
// widen who may write it: both routes are whole-route CapabilityLocal
// (owner-loopback only, refused on every remote-exposed listener, exactly
// like /api/terminal/policy), the writes require the per-load double-submit
// confirm token + application/json, and every write is audited. That is the
// same authority as editing config.toml by hand on this machine. The remote
// SELECT path (/api/terminal/ssh) stays a selector that can never add a
// profile.
//
// VALIDATION is the config loader's own: every profile goes through
// config.SSHProfile(p).ValidateWithFS (the internal/sshprofile closed
// vocabulary plus a stat of key_path; the key file's bytes are never read),
// names must be unique, and the WHOLE write is refused on the first bad entry,
// so the persisted list is always loadable. There is no ProxyCommand field
// (by design, see config.SSHProfileConfig.Jump).
//
// BINDING: the daemon's live terminal config holder re-reads the file, so a
// save binds on the next SSH launch or instance-switcher call with no restart
// (and a forward to an edited or removed profile closes).

// terminalSSHProfilePayload is one editable profile on the wire. key_path is
// a PATH only: the editor shows and edits the path; nothing ever reads or
// returns the key file's contents.
type terminalSSHProfilePayload struct {
	Name             string `json:"name"`
	Label            string `json:"label"`
	Host             string `json:"host"`
	User             string `json:"user"`
	Port             int    `json:"port"`
	KeyPath          string `json:"key_path"`
	Jump             string `json:"jump"`
	DashboardPort    int    `json:"dashboard_port"`
	ReverseProxy     bool   `json:"reverse_proxy"`
	ReverseProxyPort int    `json:"reverse_proxy_port"`
}

func (p terminalSSHProfilePayload) toConfig() config.SSHProfileConfig {
	return config.SSHProfileConfig{
		Name:             strings.TrimSpace(p.Name),
		Label:            strings.TrimSpace(p.Label),
		Host:             strings.TrimSpace(p.Host),
		User:             strings.TrimSpace(p.User),
		Port:             p.Port,
		KeyPath:          strings.TrimSpace(p.KeyPath),
		Jump:             strings.TrimSpace(p.Jump),
		DashboardPort:    p.DashboardPort,
		ReverseProxy:     p.ReverseProxy,
		ReverseProxyPort: p.ReverseProxyPort,
	}
}

func sshProfilePayloadFrom(c config.SSHProfileConfig) terminalSSHProfilePayload {
	return terminalSSHProfilePayload{
		Name: c.Name, Label: c.Label, Host: c.Host, User: c.User, Port: c.Port,
		KeyPath: c.KeyPath, Jump: c.Jump, DashboardPort: c.DashboardPort,
		ReverseProxy: c.ReverseProxy, ReverseProxyPort: c.ReverseProxyPort,
	}
}

// terminalSSHProfilesPut is the PUT body: the switch and the WHOLE list
// (replace semantics, so add / edit / remove are one validated write).
type terminalSSHProfilesPut struct {
	Enabled  *bool                       `json:"enabled,omitempty"`
	Profiles []terminalSSHProfilePayload `json:"profiles"`
}

// decodeSSHProfileBody is decodeJSONBody made STRICT: an unknown field is a
// 400, never silently dropped, so a client that believes it can set, say, a
// proxy command learns it cannot instead of saving a profile without it.
func decodeSSHProfileBody(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// sshProfileTester is the OPTIONAL LaunchManager seam the draft test-connect
// rides (the cmd adapter's TestSSHProfileDraft over sshforward.TestProfile).
type sshProfileTester interface {
	TestSSHProfileDraft(p config.SSHProfileConfig) (InstanceTestResult, error)
}

// handleTerminalSSHProfiles serves GET/PUT /api/terminal/ssh/profiles.
func (s *Server) handleTerminalSSHProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleTerminalSSHProfilesGet(w, r)
	case http.MethodPut:
		s.handleTerminalSSHProfilesPut(w, r)
	default:
		http.Error(w, "GET or PUT only", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTerminalSSHProfilesGet(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"confirm_token":            setConfirmCookie(w, r),
		"config_writable":          s.opts.ConfigPath != "",
		"terminal_enabled":         true,
		"enabled":                  true,
		"profiles":                 []terminalSSHProfilePayload{},
		"restart_required_on_save": s.launchPolicyRestartRequired(),
		"test_available":           s.sshProfileTesterAvailable(),
	}
	if cfg, err := loadConfigForDashboard(s.opts.ConfigPath); err == nil {
		resp["terminal_enabled"] = cfg.Terminal.Enabled
		resp["enabled"] = cfg.Terminal.SSH.Enabled
		rows := make([]terminalSSHProfilePayload, 0, len(cfg.Terminal.SSH.Profiles))
		for _, p := range cfg.Terminal.SSH.Profiles {
			rows = append(rows, sshProfilePayloadFrom(p))
		}
		resp["profiles"] = rows
	}
	writeJSON(w, resp)
}

// validateSSHProfiles runs the config loader's own rules over a requested
// list: per-profile ValidateWithFS and unique names. It returns the
// normalized list or the first error, naming the entry.
func validateSSHProfiles(in []terminalSSHProfilePayload) ([]config.SSHProfileConfig, error) {
	out := make([]config.SSHProfileConfig, 0, len(in))
	seen := make(map[string]bool, len(in))
	for i, raw := range in {
		p := raw.toConfig()
		label := p.Name
		if label == "" {
			label = fmt.Sprintf("#%d", i+1)
		}
		if err := config.SSHProfile(p).ValidateWithFS(); err != nil {
			return nil, fmt.Errorf("profile %s: %w", label, err)
		}
		if seen[p.Name] {
			return nil, fmt.Errorf("profile %s: duplicate name - each remote system needs its own name", label)
		}
		seen[p.Name] = true
		out = append(out, p)
	}
	return out, nil
}

func (s *Server) handleTerminalSSHProfilesPut(w http.ResponseWriter, r *http.Request) {
	if !requireJSONConfirm(w, r) {
		return
	}
	if s.opts.ConfigPath == "" {
		http.Error(w, "config path not configured - server has no file to save to", http.StatusConflict)
		return
	}
	var body terminalSSHProfilesPut
	if err := decodeSSHProfileBody(r, &body); err != nil {
		http.Error(w, "decode SSH profiles: "+err.Error(), http.StatusBadRequest)
		return
	}
	profiles, err := validateSSHProfiles(body.Profiles)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// The ONE save pipeline (REVIEW-3 P3-18): ?dry_run=1 previews the masked
	// diff and writes nothing, ?base_etag= refuses a file that changed since
	// that preview, and the write is read back and audited like every
	// section save.
	enabled := false
	s.commitConfigEdit(w, r, configEdit{
		section: "terminal.ssh",
		apply: func(cfg *config.Config) {
			if body.Enabled != nil {
				cfg.Terminal.SSH.Enabled = *body.Enabled
			}
			cfg.Terminal.SSH.Profiles = profiles
			enabled = cfg.Terminal.SSH.Enabled
		},
		// Metadata only: counts and the switch, never a host, user or key path.
		auditAction: "terminal_ssh_profiles",
		auditDetail: fmt.Sprintf("enabled=%v profiles=%d", body.Enabled != nil && *body.Enabled, len(profiles)),
		extra: func(cfg config.Config) map[string]any {
			rows := make([]terminalSSHProfilePayload, 0, len(cfg.Terminal.SSH.Profiles))
			for _, p := range cfg.Terminal.SSH.Profiles {
				rows = append(rows, sshProfilePayloadFrom(p))
			}
			return map[string]any{
				"enabled":          enabled,
				"profiles":         rows,
				"restart_required": s.launchPolicyRestartRequired(),
			}
		},
	})
}

func (s *Server) sshProfileTesterAvailable() bool {
	_, ok := s.opts.LaunchManager.(sshProfileTester)
	return ok
}

// handleTerminalSSHProfileTest serves POST /api/terminal/ssh/profiles/test:
// a bounded, read-only connectivity probe (ssh -o BatchMode=yes ... exit) of
// a DRAFT profile, so the editor can test before saving. Confirm-token gated
// like the writes (it reaches another machine), and the draft is validated
// with the same rules as a save before anything runs.
func (s *Server) handleTerminalSSHProfileTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !requireJSONConfirm(w, r) {
		return
	}
	var body terminalSSHProfilePayload
	if err := decodeSSHProfileBody(r, &body); err != nil {
		http.Error(w, "decode SSH profile: "+err.Error(), http.StatusBadRequest)
		return
	}
	draft, err := validateSSHProfiles([]terminalSSHProfilePayload{body})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tester, ok := s.opts.LaunchManager.(sshProfileTester)
	if !ok {
		http.Error(w, "SSH test-connect is not available on this daemon (the embedded terminal stack is not running)", http.StatusNotImplemented)
		return
	}
	result, err := tester.TestSSHProfileDraft(draft[0])
	if err != nil {
		writeInstanceErr(w, err)
		return
	}
	s.recordManageAudit(r, "terminal_ssh_profile_test", fmt.Sprintf("auth_ok=%v", result.AuthOK))
	writeJSON(w, result)
}
