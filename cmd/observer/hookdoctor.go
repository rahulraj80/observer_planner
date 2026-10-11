package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/diag"
	"github.com/marmutapp/superbased-observer/internal/hook"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// `observer doctor --hooks [tool]` (hook-parity Lane G): per BINDING, on
// this machine, is Observer's hook registered in the vendor's config, has the
// host trusted it (Codex `[hooks.state]`), and, with --probe-hook, did a
// synthetic call through each wired gate block. Read-only: registration is
// asked of the same writers `observer init` uses in DryRun mode, trust is a
// file read, and only --probe-hook runs a hook (recording one guard verdict
// under a probe session id). Every decision dispatches on the binding row
// and its mechanism, never a tool name, so new bindings appear here as soon
// as another lane adds them.

// Probe gate selections for --gate.
const (
	probeGateAll    = "all"
	probeGatePrompt = "prompt"
	probeGateTool   = "tool"
)

// hookDoctorOptions is one `observer doctor --hooks / --probe-hook` run.
type hookDoctorOptions struct {
	Binary, ConfigPath string
	// Tool scopes the run to one adapter ("" = every candidate).
	Tool string
	// Bindings reports per-binding registration / trust (--hooks).
	Bindings bool
	// Probe fires the synthetic gate probes (--probe-hook).
	Probe bool
	// Gate selects which gate probes run: all / prompt / tool.
	Gate string
	JSON bool
	// Lookup reads the guard verdicts a probe session recorded.
	Lookup guardEventLookup
}

// storeGuardEventLookup is the production guardEventLookup.
func storeGuardEventLookup(st *store.Store) guardEventLookup {
	return func(ctx context.Context, sessionID string) ([]string, error) {
		rows, err := st.LoadGuardEventsForSession(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		var out []string
		for _, r := range rows {
			out = append(out, r.RuleID)
		}
		return out, nil
	}
}

// runHookDoctor runs the binding report and / or the probes and prints the
// report. Returns an error when any check failed (the doctor exit code).
func runHookDoctor(ctx context.Context, w io.Writer, o hookDoctorOptions) error {
	gate := o.Gate
	if gate == "" {
		gate = probeGateAll
	}
	switch gate {
	case probeGateAll, probeGatePrompt, probeGateTool:
	default:
		return fmt.Errorf("--gate must be all, prompt or tool (got %q)", gate)
	}
	var checks []diag.Check
	switch {
	case o.Bindings:
		tools := []string{o.Tool}
		if o.Tool == "" {
			tools = hookDoctorCandidates()
		}
		for _, tool := range tools {
			checks = append(checks, hookBindingChecks(ctx, o, tool, gate)...)
		}
	case o.Probe:
		checks = hookProbeChecks(ctx, o, gate)
	}
	report := diag.Report{Checks: checks}
	if o.JSON {
		body, _ := json.MarshalIndent(report, "", "  ")
		fmt.Fprintln(w, string(body))
	} else {
		printReport(w, report)
	}
	if report.Failed() {
		return errors.New("one or more checks failed")
	}
	return nil
}

// hookProbeChecks runs the gate probes alone (--probe-hook without --hooks).
func hookProbeChecks(ctx context.Context, o hookDoctorOptions, gate string) []diag.Check {
	var checks []diag.Check
	if gate != probeGateTool {
		tools := []string{o.Tool}
		if o.Tool == "" {
			tools = probeHookToolCandidates()
		}
		for _, tool := range tools {
			if o.Tool != "" && !hasWiredGate(tool, integration.HookCapPromptGate) && gate == probeGateAll {
				continue
			}
			checks = append(checks, runProbeHook(ctx, o.Binary, o.ConfigPath, tool))
		}
	}
	if gate != probeGatePrompt {
		tools := []string{o.Tool}
		if o.Tool == "" {
			tools = toolGateProbeCandidates()
		}
		for _, tool := range tools {
			if o.Tool != "" && !hasWiredGate(tool, integration.HookCapToolGate) && gate == probeGateAll {
				continue
			}
			checks = append(checks, runToolGateProbes(ctx, o.Binary, o.ConfigPath, tool, o.Lookup)...)
		}
	}
	if len(checks) == 0 && o.Tool != "" {
		checks = append(checks, diag.Check{
			Name: "guard.probe:" + o.Tool, Status: diag.StatusWarn,
			Message: fmt.Sprintf("%s: no wired gate hook to probe (see `observer adapters --hooks`)", o.Tool),
		})
	}
	return checks
}

func hasWiredGate(tool string, cp integration.HookCap) bool {
	c, ok := integration.For(tool)
	return ok && c.Hook.WiredFor(cp)
}

// hookDoctorCandidates is every registry tool with a hook mechanism, sorted.
func hookDoctorCandidates() []string {
	var out []string
	for _, c := range integration.Capabilities() {
		if c.Hook.Mechanism != integration.HookNone {
			out = append(out, c.Tool)
		}
	}
	sort.Strings(out)
	return out
}

// hookHomeEvents is one config home's registration read: the native events
// Observer's hook occupies there.
type hookHomeEvents struct {
	home, tool string
	present    map[string]bool
	conflict   bool
	// skipped is the writer's SkipReason; covered is true only when the
	// skip means another surface carries Observer's wiring (the Claude
	// Code plugin). A not-applicable skip (an unsupported OS, a suppressed
	// cross-OS target) means there is no Observer hook in this home at all
	// (review M2-05).
	skipped string
	covered bool
	detail  string
	err     error
}

// readHookHomeEvents asks the registration writer (DryRun) once per home
// which events already carry Observer's command.
func readHookHomeEvents(binary, configPath, tool string) []hookHomeEvents {
	read := func(home, target string) hookHomeEvents {
		h := hookHomeEvents{home: home, tool: target, present: map[string]bool{}}
		// Union over the equivalent --config spellings; a conflict or a
		// skip is taken from the first (the caller's own) spelling.
		for i, spelling := range registrationConfigSpellings(configPath) {
			reg, err := hook.NewRegistry(hookRegistrationOptions(binary, spelling))
			if err != nil {
				h.err = err
				return h
			}
			res := reg.Register(target)
			if i == 0 {
				h.detail = res.ConfigPath
				switch {
				case res.Skipped:
					h.skipped = res.SkipReason
					h.covered = res.CoveredElsewhere()
				case res.Error != nil:
					h.conflict = true
					h.detail = res.Error.Error()
				}
			}
			for _, ev := range res.AlreadySet {
				h.present[ev] = true
			}
		}
		return h
	}
	out := []hookHomeEvents{read(hookHomeNative, tool)}
	win := tool + "-windows"
	if hookSupported(win) && windowsHookHomeDetected(binary, win) {
		out = append(out, read(hookHomeBridge, win))
	}
	return out
}

// hookInstalled reports whether the tool's config home exists on this
// machine (the writers' own detection).
func hookInstalled(binary, configPath, tool string) bool {
	reg, err := hook.NewRegistry(hookRegistrationOptions(binary, configPath))
	if err != nil {
		return true // unknown: do not hide the report
	}
	for _, t := range reg.Installed() {
		if t == tool || t == tool+"-windows" {
			return true
		}
	}
	return false
}

// bindingRegistration is one binding's registration verdict across homes:
// "yes" (registered as Observer writes it today, or covered by another
// surface that carries Observer's wiring), "conflict", "n/a" (no Observer
// hook can exist in any home read, review M2-05), "no", "unknown" or
// "manual".
func bindingRegistration(c integration.Capability, b integration.HookBinding, homes []hookHomeEvents) (state, where string) {
	if !c.Hook.AutoWired {
		return "manual", "no registration writer: wired by hand or by the plugin's own install"
	}
	perEvent := b.NeedsEventConfig(c.Hook.Mechanism)
	var conflicts, notApplicable []string
	for _, h := range homes {
		switch {
		case h.err != nil:
			return "unknown", h.err.Error()
		case h.skipped != "" && h.covered:
			return "yes", h.home + ": covered by " + h.skipped
		case h.skipped != "":
			notApplicable = append(notApplicable, h.home+": "+h.skipped)
		case h.present[b.NativeEvent] || (!perEvent && len(h.present) > 0):
			return "yes", h.home + " " + h.detail
		case h.conflict:
			conflicts = append(conflicts, h.home+": "+h.detail)
		}
	}
	if len(conflicts) > 0 {
		return "conflict", strings.Join(conflicts, "; ")
	}
	if len(notApplicable) > 0 {
		return "n/a", strings.Join(notApplicable, "; ")
	}
	var where2 []string
	for _, h := range homes {
		where2 = append(where2, h.home+" "+h.detail)
	}
	return "no", strings.Join(where2, "; ")
}

// bindingCheckRules is the ordered status table for one binding check: the
// first matching row decides.
var bindingCheckRules = []struct {
	match  func(reg, trust, probe string) bool
	status diag.Status
}{
	{func(reg, _, probe string) bool { return reg == "conflict" || probe == "fail" }, diag.StatusFail},
	{func(reg, trust, probe string) bool {
		return reg == "no" || reg == "unknown" || trust == "no" || probe == "warn"
	}, diag.StatusWarn},
	{func(string, string, string) bool { return true }, diag.StatusOK},
}

// hookBindingChecks reports every registered or wired binding of tool.
func hookBindingChecks(ctx context.Context, o hookDoctorOptions, tool, gate string) []diag.Check {
	c, ok := integration.For(tool)
	if !ok {
		return []diag.Check{{Name: "hook.bindings:" + tool, Status: diag.StatusWarn, Message: fmt.Sprintf("%q is not a registry adapter", tool)}}
	}
	summary := diag.Check{Name: "hook.bindings:" + tool, Status: diag.StatusOK}
	if c.Hook.Mechanism == integration.HookNone {
		summary.Message = tool + ": no hook mechanism Observer registers"
		if c.Hook.Note != "" {
			summary.Message += " (" + c.Hook.Note + ")"
		}
		return []diag.Check{summary}
	}
	var mine, vendorOnly []integration.HookBinding
	for _, b := range c.Hook.Bindings {
		if b.Registered || b.Wired {
			mine = append(mine, b)
		} else {
			vendorOnly = append(vendorOnly, b)
		}
	}
	if c.Hook.AutoWired && !hookInstalled(o.Binary, o.ConfigPath, tool) {
		summary.Message = fmt.Sprintf("%s: not installed on this machine (no config home found); %d binding(s) would be registered by `observer init`", tool, len(mine))
		return []diag.Check{summary}
	}
	homes := readHookHomeEvents(o.Binary, o.ConfigPath, tool)
	trust := diag.HookTrustFor(c.Hook.Mechanism, probeHookHome(o.Binary, o.ConfigPath))

	probes := map[string]diag.Check{}
	if o.Probe {
		probes = hookBindingProbes(ctx, o, c, tool, gate, mine)
	}

	out := []diag.Check{}
	registered, wired := 0, 0
	for _, b := range mine {
		ck, reg := bindingCheck(c, b, tool, homes, trust, probes)
		if reg == "yes" {
			registered++
		}
		if b.Wired {
			wired++
		}
		out = append(out, ck)
	}
	summary.Message = fmt.Sprintf("%s: %d of %d Observer binding(s) registered on this machine (%d wired); %d vendor-only event(s) Observer does not register",
		tool, registered, len(mine), wired, len(vendorOnly))
	if len(vendorOnly) > 0 {
		var evs []string
		for _, b := range vendorOnly {
			evs = append(evs, b.NativeEvent+" ("+string(b.Cap)+")")
		}
		summary.Details = []string{"vendor-only: " + strings.Join(evs, ", ")}
	}
	return append([]diag.Check{summary}, out...)
}

// hookBindingProbes runs the gate probes the gate selection asks for and
// keys their checks by native event.
func hookBindingProbes(ctx context.Context, o hookDoctorOptions, c integration.Capability, tool, gate string, mine []integration.HookBinding) map[string]diag.Check {
	probes := map[string]diag.Check{}
	if gate != probeGatePrompt {
		for _, ck := range runToolGateProbes(ctx, o.Binary, o.ConfigPath, tool, o.Lookup) {
			probes[strings.TrimPrefix(ck.Name, "guard.tool.probe:"+tool+":")] = ck
		}
	}
	if gate != probeGateTool && c.Hook.WiredFor(integration.HookCapPromptGate) {
		ck := runProbeHook(ctx, o.Binary, o.ConfigPath, tool)
		for _, b := range mine {
			if b.Cap == integration.HookCapPromptGate && b.Wired {
				probes[b.NativeEvent] = ck
				break
			}
		}
	}
	return probes
}

// bindingTrusted is one binding's trust verdict: n/a where the tool has no
// trust step or the binding is not registered.
func bindingTrusted(trust diag.HookTrust, b integration.HookBinding, reg string) string {
	switch {
	case !trust.Applicable || reg != "yes":
		return "n/a"
	case trust.Err != "":
		return "unknown"
	case trust.Trusted[b.NativeEvent]:
		return "yes"
	default:
		return "no"
	}
}

// bindingCheck is one binding's check and its registration verdict.
func bindingCheck(c integration.Capability, b integration.HookBinding, tool string, homes []hookHomeEvents, trust diag.HookTrust, probes map[string]diag.Check) (diag.Check, string) {
	reg, where := bindingRegistration(c, b, homes)
	trusted := bindingTrusted(trust, b, reg)
	probe, probeMsg := "n/a", ""
	if b.Cap.IsGate() && b.Wired {
		probe = "not run (add --probe-hook)"
		if ck, ok := probes[b.NativeEvent]; ok {
			probe = strings.ToLower(ck.Status.String())
			probeMsg = ck.Message
		}
	}
	ck := diag.Check{
		Name: fmt.Sprintf("hook.binding:%s:%s:%s", tool, b.NativeEvent, b.Cap),
		Message: fmt.Sprintf("%s %s (%s%s): registered=%s, trusted=%s, probe=%s",
			tool, b.NativeEvent, b.Cap, wiredSuffix(b), reg, trusted, probe),
		Details: []string{"where: " + where, "grounding: " + string(b.Grounding.Kind) + " (" + b.Grounding.Source + ")"},
	}
	if trusted == "no" {
		ck.Details = append(ck.Details, "remedy: "+trust.Remedy)
	}
	if reg == "no" {
		ck.Details = append(ck.Details, "remedy: "+autoRegisterRemediation(tool))
	}
	if probeMsg != "" {
		ck.Details = append(ck.Details, "probe: "+probeMsg)
	}
	for _, r := range bindingCheckRules {
		if r.match(reg, trusted, probe) {
			ck.Status = r.status
			break
		}
	}
	return ck, reg
}

func wiredSuffix(b integration.HookBinding) string {
	if b.Wired {
		return ", wired"
	}
	return ", registered only"
}
