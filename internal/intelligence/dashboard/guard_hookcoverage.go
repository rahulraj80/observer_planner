package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/controlcoverage"
	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/guardposture"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// Hook capability coverage on the node Security page (hook-parity Lane G):
// the dashboard face of `observer adapters --hooks` (the matrix, generated
// from the registry's bindings through internal/controlcoverage), of
// `observer doctor --hooks` (per-binding registration / trust on this
// machine) and of `observer doctor --probe-hook` for gate hooks, plus the
// node's guard posture and the gate hook failures the daemon recorded
// (T-049 fail-open and fail-closed). The CLI actions run through the SAME
// CLI code by self-exec (the guard_prompt.go precedent), so there is one
// implementation; the probe records a guard verdict, so it is Local-only
// and needs the confirm token.

type hookFailureJSON struct {
	Tool    string `json:"tool"`
	Outcome string `json:"outcome"` // fail_closed | fail_open
	Kind    string `json:"kind"`
	Count   int    `json:"count"`
	LastAt  string `json:"last_at,omitempty"`
}

type hookPostureJSON struct {
	// State is absent / valid / stale / tampered.
	State      string   `json:"state"`
	Detail     string   `json:"detail,omitempty"`
	Enforced   bool     `json:"enforced"`
	GateCaps   []string `json:"gate_caps"`
	Source     string   `json:"source,omitempty"`
	Generation string   `json:"generation,omitempty"`
	WrittenAt  string   `json:"written_at,omitempty"`
}

type hookCoverageResponse struct {
	Matrix       controlcoverage.MatrixView `json:"matrix"`
	Posture      hookPostureJSON            `json:"posture"`
	Failures     []hookFailureJSON          `json:"failures"`
	WindowHours  int                        `json:"window_hours"`
	ConfirmToken string                     `json:"confirm_token,omitempty"`
}

// hookPostureView reads the node's guard posture the way a hook does
// (guardposture.Load + Stale), for display.
func hookPostureView(configPath string, now time.Time) hookPostureJSON {
	out := hookPostureJSON{State: "absent", GateCaps: []string{}}
	resolved, err := config.ResolveGlobalPath(configPath)
	if err != nil {
		out.Detail = err.Error()
		return out
	}
	ld := guardposture.Load(guardposture.PosturePath(resolved), guardposture.KeyPath(resolved))
	switch ld.State {
	case guardposture.StateTampered:
		out.State, out.Detail = "tampered", ld.Detail
		return out
	case guardposture.StateAbsent:
		out.Detail = "no daemon has published a posture on this node; hooks follow the config"
		return out
	}
	p := ld.Posture
	out.State = "valid"
	var mod time.Time
	if st, err := os.Stat(resolved); err == nil {
		mod = st.ModTime()
	}
	if why := p.Stale(now, mod); why != "" {
		out.State, out.Detail = "stale", why
	}
	out.Enforced, out.Source, out.Generation = p.Enforced, p.Source, p.Generation
	if p.GateCaps != nil {
		out.GateCaps = p.GateCaps
	}
	if !p.WrittenAt.IsZero() {
		out.WrittenAt = p.WrittenAt.UTC().Format(time.RFC3339)
	}
	return out
}

// handleGuardHookCoverage serves GET /api/guard/hook-coverage?hours= .
func (s *Server) handleGuardHookCoverage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	hours := 168
	if v, err := strconv.Atoi(r.URL.Query().Get("hours")); err == nil && v > 0 && v <= 24*90 {
		hours = v
	}
	now := s.now()
	counts, err := store.New(s.opts.DB).HookFailureCounts(r.Context(), guard.GuardErrorRuleID, guardposture.FailureKinds(), now.Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		writeErr(w, err)
		return
	}
	failures := make([]hookFailureJSON, 0, len(counts))
	for _, c := range counts {
		f := hookFailureJSON{Tool: c.Tool, Outcome: guardposture.OutcomeFailOpen, Kind: c.Kind, Count: c.Count}
		if c.FailClosed {
			f.Outcome = guardposture.OutcomeFailClosed
		}
		if !c.LastAt.IsZero() {
			f.LastAt = c.LastAt.UTC().Format(time.RFC3339)
		}
		failures = append(failures, f)
	}
	writeJSON(w, hookCoverageResponse{
		Matrix:       controlcoverage.HookMatrixView(),
		Posture:      hookPostureView(s.opts.ConfigPath, now),
		Failures:     failures,
		WindowHours:  hours,
		ConfirmToken: setConfirmCookie(w, r),
	})
}

// hookToolRequest is the body of the doctor / probe actions.
type hookToolRequest struct {
	Tool string `json:"tool"`
	Gate string `json:"gate"`
}

// decodeHookToolRequest reads and validates {tool, gate}: the tool must be
// a registry adapter (never an arbitrary argv), the gate a known value.
func decodeHookToolRequest(r *http.Request, w http.ResponseWriter) (hookToolRequest, bool) {
	var req hookToolRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return req, false
	}
	if _, ok := integration.For(req.Tool); !ok {
		http.Error(w, fmt.Sprintf("unknown tool %q", req.Tool), http.StatusBadRequest)
		return req, false
	}
	switch req.Gate {
	case "":
		req.Gate = "all"
	case "all", "prompt", "tool":
	default:
		http.Error(w, "gate must be all, prompt or tool", http.StatusBadRequest)
		return req, false
	}
	return req, true
}

// runSelfDoctor execs `<this binary> doctor <args> --json [--config]` and
// returns its checks (the probe / doctor exit non-zero on a failed check,
// which is a result, not an error).
func (s *Server) runSelfDoctor(ctx context.Context, args []string) ([]promptProbeCheckJSON, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve this binary's own path: %w", err)
	}
	args = append(append([]string{"doctor"}, args...), "--json")
	if s.opts.ConfigPath != "" {
		args = append(args, "--config", s.opts.ConfigPath)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...) //nolint:gosec // G204: exe is this process's own path; args are fixed literals plus a registry-validated tool name
	out, runErr := cmd.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("doctor timed out")
	}
	if len(out) == 0 && runErr != nil {
		return nil, fmt.Errorf("doctor failed to run: %w", runErr)
	}
	var report diagJSONReport
	if err := json.Unmarshal(out, &report); err != nil {
		return nil, fmt.Errorf("decode doctor output: %w", err)
	}
	checks := make([]promptProbeCheckJSON, 0, len(report.Checks))
	for _, c := range report.Checks {
		checks = append(checks, promptProbeCheckJSON{Name: c.Name, Status: c.Status.String(), Message: c.Message, Details: c.Details})
	}
	return checks, nil
}

// handleGuardHookDoctor serves POST /api/guard/hook-doctor {tool}: the
// per-binding registration / trust report (`observer doctor --hooks
// <tool>`). Read-only (the registration read is the writers' DryRun).
func (s *Server) handleGuardHookDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	req, ok := decodeHookToolRequest(r, w)
	if !ok {
		return
	}
	checks, err := s.runSelfDoctor(r.Context(), []string{"--hooks", req.Tool})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"checks": checks})
}

// handleGuardHookProbe serves POST /api/guard/hook-probe {tool, gate}: the
// per-binding report with the synthetic gate probes (`observer doctor
// --hooks --probe-hook --gate <gate> <tool>`). Each probe records one guard
// verdict under a probe session id, so the action needs the confirm token.
func (s *Server) handleGuardHookProbe(w http.ResponseWriter, r *http.Request) {
	if !requireConfirmToken(w, r) {
		return
	}
	req, ok := decodeHookToolRequest(r, w)
	if !ok {
		return
	}
	checks, err := s.runSelfDoctor(r.Context(), []string{"--hooks", "--probe-hook", "--gate", req.Gate, req.Tool})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"checks": checks})
}
