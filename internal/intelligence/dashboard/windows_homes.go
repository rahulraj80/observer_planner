package dashboard

import (
	"fmt"
	"net/http"

	"github.com/marmutapp/superbased-observer/internal/diag"
	"github.com/marmutapp/superbased-observer/internal/remedy"
)

// Windows-home init (T-421 item 4): the dashboard twin of `observer init
// --windows-claude-home | --windows-codex-home | --windows-cursor-home <path>`
// for a WSL daemon whose AI tools run on Windows. When several Windows homes
// carry a tool's config (or the one home cannot be verified as the current
// Windows user's), `observer init` refuses to auto-pick and the doctor's
// "windows proxy routes" finding asks the operator to choose. This control
// lists the detected homes, previews the exact writes for the chosen one
// (a dry run), then applies them through the same code path the CLI flag
// takes (cmd's wireAIClients with the home override), scoped to the
// Windows-side target.

// windowsHomeTargetWire is one target as GET /api/setup/windows-homes
// spells it: the detection plus the CLI flag the same write takes.
type windowsHomeTargetWire struct {
	diag.WindowsHomeTarget
	Flag    string `json:"flag"`
	Command string `json:"command"`
}

type windowsHomeApplyBody struct {
	Target string `json:"target"`
	Home   string `json:"home"`
	DryRun bool   `json:"dry_run"`
	Force  bool   `json:"force"`
}

// handleWindowsHomes serves GET /api/setup/windows-homes: whether this is a
// WSL daemon and the Windows homes per cross-OS target. Owner-local (it names
// the operator's Windows home directories) and it mints the confirm token
// the apply requires.
func (s *Server) handleWindowsHomes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if s.opts.WindowsHomes == nil {
		http.Error(w, "Windows-home init is not wired on this dashboard", http.StatusNotImplemented)
		return
	}
	tok := setConfirmCookie(w, r)
	wsl, targets, err := s.opts.WindowsHomes.Targets(r.Context())
	if err != nil {
		writeErr(w, fmt.Errorf("windows homes: %w", err))
		return
	}
	out := make([]windowsHomeTargetWire, 0, len(targets))
	for _, t := range targets {
		flag, _ := remedy.WindowsHomeFlag(t.Target)
		out = append(out, windowsHomeTargetWire{
			WindowsHomeTarget: t,
			Flag:              flag,
			Command:           remedy.Command(remedy.Of(remedy.InitWindowsHome, t.Target)),
		})
	}
	writeJSON(w, map[string]any{"confirm_token": tok, "wsl": wsl, "targets": out})
}

// handleWindowsHomesApply serves POST /api/setup/windows-homes/apply: preview
// (dry_run) or apply the Windows-side writes for one target and one detected
// home. A home that is not a detected candidate is refused: the control never
// writes into an arbitrary path.
func (s *Server) handleWindowsHomesApply(w http.ResponseWriter, r *http.Request) {
	if !requireConfirmToken(w, r) {
		return
	}
	if s.opts.WindowsHomes == nil {
		http.Error(w, "Windows-home init is not wired on this dashboard", http.StatusNotImplemented)
		return
	}
	var body windowsHomeApplyBody
	if err := decodeJSONBody(r, &body); err != nil {
		http.Error(w, "decode windows-home apply: "+err.Error(), http.StatusBadRequest)
		return
	}
	wsl, targets, err := s.opts.WindowsHomes.Targets(r.Context())
	if err != nil {
		writeErr(w, fmt.Errorf("windows homes: %w", err))
		return
	}
	if !wsl {
		http.Error(w, "Windows-side homes exist only for a daemon running inside WSL", http.StatusConflict)
		return
	}
	if !windowsHomeIsCandidate(targets, body.Target, body.Home) {
		http.Error(w, fmt.Sprintf("%q is not a detected Windows home for %q", body.Home, body.Target), http.StatusBadRequest)
		return
	}
	s.configWriteMu.Lock()
	lines, err := s.opts.WindowsHomes.Apply(r.Context(), WindowsHomeApply{Target: body.Target, Home: body.Home, DryRun: body.DryRun, Force: body.Force})
	s.configWriteMu.Unlock()
	resp := map[string]any{"target": body.Target, "home": body.Home, "dry_run": body.DryRun, "lines": nonNilLines(lines)}
	if err != nil {
		resp["error"] = err.Error()
		writeJSONStatus(w, http.StatusConflict, resp)
		return
	}
	if !body.DryRun {
		s.recordManageAudit(r, "windows_home_init", fmt.Sprintf("target=%s lines=%d", body.Target, len(lines)))
	}
	writeJSON(w, resp)
}

// windowsHomeIsCandidate reports whether home is a detected candidate for
// target.
func windowsHomeIsCandidate(targets []diag.WindowsHomeTarget, target, home string) bool {
	for _, t := range targets {
		if t.Target != target {
			continue
		}
		for _, c := range t.Candidates {
			if c.Home == home {
				return true
			}
		}
	}
	return false
}

func nonNilLines(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}
