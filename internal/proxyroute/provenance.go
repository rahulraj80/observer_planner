package proxyroute

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/routeprov"
)

// ProvenanceFileName is the route-provenance state file, kept in the
// registrar home's .observer directory (beside the default config.toml).
// proxyroute is its one owner: the registrar that writes a route records
// who wrote it, and Leave reads it back (internal/routeprov holds the rules).
const ProvenanceFileName = "route-provenance.json"

// ProvenancePath is where this registrar keeps route provenance.
func (r *Registrar) ProvenancePath() string {
	return filepath.Join(r.opts.HomeDir, ".observer", ProvenanceFileName)
}

func loadProvenance(path string) (routeprov.File, error) {
	f := routeprov.File{Version: routeprov.FileVersion}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, fmt.Errorf("proxyroute.loadProvenance: %w", err)
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return f, fmt.Errorf("proxyroute.loadProvenance: parse %s: %w", path, err)
	}
	return f, nil
}

func saveProvenance(path string, f routeprov.File) error {
	f.Version = routeprov.FileVersion
	body, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("proxyroute.saveProvenance: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("proxyroute.saveProvenance: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("proxyroute.saveProvenance: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("proxyroute.saveProvenance: rename: %w", err)
	}
	return nil
}

// note records who wrote or confirmed a route (routeprov.File.Note's claim
// rule). want is the value this writer writes: an "already set" result for
// a DIFFERENT value (another observer install's port) is not a confirmation
// of ours. Best-effort: a provenance failure never fails the route write,
// it is reported on the result, and the safe direction holds (no record
// means Leave keeps the route).
func (r *Registrar) note(res RegistrationResult, want string) RegistrationResult {
	outcome := routeprov.OutcomeNone
	switch {
	case res.DryRun || res.Error != nil || res.ConfigMissing || res.ConfigPath == "":
	case res.Added:
		outcome = routeprov.OutcomeWrote
	case res.AlreadySet && res.BaseURL == want:
		outcome = routeprov.OutcomeConfirmed
	}
	if outcome == routeprov.OutcomeNone {
		return res
	}
	path := r.ProvenancePath()
	f, err := loadProvenance(path)
	if err != nil {
		res.ProvenanceError = err
		return res
	}
	changed := f.Note(routeprov.Record{
		Tool:          res.Tool,
		ConfigPath:    res.ConfigPath,
		Value:         want,
		Writer:        r.opts.Writer,
		PriorProvider: res.PriorProvider,
		WrittenAt:     time.Now().UTC().Format(time.RFC3339),
	}, outcome)
	if changed {
		if err := saveProvenance(path, f); err != nil {
			res.ProvenanceError = err
		}
	}
	return res
}

// routeShape is how Leave reads and removes one tool's route. A tool without
// a shape is recorded but never removed; Leave names it so the developer can
// remove it by hand. Every tool proxyroute writes a persisted route for has a
// shape since T-156a (the probe-lane readers / removers live in
// probe_remove.go, beside the writers they undo).
type routeShape struct {
	read   func(path string) (string, bool, error)
	remove func(path, value, priorProvider string) error
}

var routeShapes = map[string]routeShape{
	"claude-code": {readClaudeRouteAt, removeClaudeRouteAt},
	"codex":       {readCodexRouteAt, removeCodexRouteAt},
	"kimi-code":   {readKimiRouteAt, removeKimiRouteAt},
	"qwen-code":   {readQwenRouteAt, removeQwenRouteAt},
	"crush":       {readCrushRouteAt, removeCrushRouteAt},
}

func shapeFor(tool string) (routeShape, bool) {
	s, ok := routeShapes[strings.TrimSuffix(tool, "-windows")]
	return s, ok
}

// LeaveRoute is Leave's verdict, and what happened, for one route target.
type LeaveRoute struct {
	routeprov.Decision
	// Removed is set when the apply pass removed the route.
	Removed bool `json:"removed"`
	// Error is set when the removal (or the read) failed; the route stays.
	Error string `json:"error,omitempty"`
}

// LeaveRoutes is the proxy-route part of leaving an organization, for the
// CLI's `observer unenroll` and the dashboard's Leave alike. With apply
// false it only plans (the Leave dialog's list); with apply true it removes
// every route Observer's enrolment wrote that is still exactly what it wrote
// (routeprov.Decide) and drops those records. Routes you, `observer init`,
// Settings > Setup or a launcher set are kept. The native Claude Code and
// Codex routes are always reported when they point at an observer, so the
// dialog can say a route will be kept, not just stay silent.
func (r *Registrar) LeaveRoutes(apply bool) ([]LeaveRoute, error) {
	path := r.ProvenancePath()
	f, err := loadProvenance(path)
	if err != nil {
		return nil, err
	}
	type target struct{ tool, path string }
	targets := []target{}
	seen := map[target]bool{}
	add := func(t target) {
		if !seen[t] {
			seen[t] = true
			targets = append(targets, t)
		}
	}
	for _, rec := range f.Records {
		add(target{rec.Tool, rec.ConfigPath})
	}
	add(target{"claude-code", filepath.Join(r.opts.HomeDir, ".claude", "settings.json")})
	add(target{"codex", filepath.Join(r.opts.HomeDir, ".codex", "config.toml")})

	var out []LeaveRoute
	changed := false
	for _, t := range targets {
		rec := f.Find(t.tool, t.path)
		shape, removable := shapeFor(t.tool)
		in := routeprov.DecideInput{Tool: t.tool, ConfigPath: t.path, Record: rec, Removable: removable, Setting: integration.RouteSetting(t.tool)}
		var readErr error
		switch {
		case removable:
			in.Current, in.Present, readErr = shape.read(t.path)
		case rec != nil:
			// No reader for this tool: report what enrolment recorded.
			in.Current, in.Present = rec.Value, true
		}
		if readErr != nil {
			out = append(out, LeaveRoute{
				Decision: routeprov.Decision{Tool: t.tool, ConfigPath: t.path, Action: routeprov.ActionKeep},
				Error:    readErr.Error(),
			})
			continue
		}
		d := routeprov.Decide(in)
		if d.Action == routeprov.ActionNone {
			if rec != nil && rec.Writer == routeprov.WriterEnrolment && apply {
				f.Drop(t.tool, t.path) // the route is gone; so is the claim
				changed = true
			}
			continue
		}
		// A route with no record is only worth a line when it is an observer
		// route (the one a developer might expect Leave to touch).
		if rec == nil && !IsObserverBaseURL(in.Current) {
			continue
		}
		lr := LeaveRoute{Decision: d}
		if apply && d.Action == routeprov.ActionRemove {
			if err := shape.remove(t.path, rec.Value, rec.PriorProvider); err != nil {
				lr.Error = err.Error()
			} else {
				lr.Removed = true
				f.Drop(t.tool, t.path)
				changed = true
			}
		}
		out = append(out, lr)
	}
	if changed {
		if err := saveProvenance(path, f); err != nil {
			return out, err
		}
	}
	return out, nil
}

// readClaudeRouteAt reads env.ANTHROPIC_BASE_URL from a Claude Code
// settings.json.
func readClaudeRouteAt(path string) (string, bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("proxyroute.readClaudeRouteAt: %w", err)
	}
	var s struct {
		Env map[string]any `json:"env"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false, fmt.Errorf("proxyroute.readClaudeRouteAt: parse %s: %w", path, err)
	}
	v, _ := s.Env["ANTHROPIC_BASE_URL"].(string)
	return v, v != "", nil
}

// removeClaudeRouteAt removes env.ANTHROPIC_BASE_URL (and the hint switch
// the writer adds, only when it is exactly the writer's value), preserving
// every other key, exactly as UnregisterClaudeCode does. It refuses when the
// value is no longer the one recorded.
func removeClaudeRouteAt(path, value, _ string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("proxyroute.removeClaudeRouteAt: read: %w", err)
	}
	settings := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return fmt.Errorf("proxyroute.removeClaudeRouteAt: parse %s: %w", path, err)
	}
	env := map[string]any{}
	if e, ok := settings["env"]; ok {
		if err := json.Unmarshal(e, &env); err != nil {
			return fmt.Errorf("proxyroute.removeClaudeRouteAt: parse env: %w", err)
		}
	}
	if cur, _ := env["ANTHROPIC_BASE_URL"].(string); cur != value {
		return fmt.Errorf("proxyroute.removeClaudeRouteAt: ANTHROPIC_BASE_URL is %q, not the %q enrolment wrote", cur, value)
	}
	delete(env, "ANTHROPIC_BASE_URL")
	if v, _ := env[ClaudeGatewayHintEnv].(string); v == claudeGatewayHintOn {
		delete(env, ClaudeGatewayHintEnv)
	}
	if len(env) == 0 {
		delete(settings, "env")
	} else {
		patched, err := json.Marshal(env)
		if err != nil {
			return fmt.Errorf("proxyroute.removeClaudeRouteAt: marshal env: %w", err)
		}
		settings["env"] = patched
	}
	return writeClaudeSettings(filepath.Dir(path), path, settings)
}

// readCodexRouteAt reads the Observer provider's base_url from a Codex
// config.toml; the route is only "set" while model_provider selects it.
func readCodexRouteAt(path string) (string, bool, error) {
	root, err := readCodexRoot(path)
	if err != nil || root == nil {
		return "", false, err
	}
	if mp, _ := root["model_provider"].(string); mp != ProviderName {
		return "", false, nil
	}
	providers, _ := root["model_providers"].(map[string]any)
	ours, _ := providers[ProviderName].(map[string]any)
	v, _ := ours["base_url"].(string)
	return v, v != "", nil
}

// removeCodexRouteAt undoes registerCodexAt: it drops the Observer provider
// block and puts model_provider back to what it was before the write
// (removing the key when it was unset). It refuses when the route is no
// longer the one recorded. The edit is surgical (T-394): cfgfmt keeps every
// comment, key order and blank line it does not touch, exactly like
// UnregisterCodex (unregisterCodexEdits is the one edit both use), and the
// file keeps its mode.
func removeCodexRouteAt(path, value, priorProvider string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("proxyroute.removeCodexRouteAt: %s is gone", path)
	}
	if err != nil {
		return fmt.Errorf("proxyroute.removeCodexRouteAt: read: %w", err)
	}
	doc, err := cfgfmt.ParseWith(cfgfmt.FormatTOML, raw, cfgfmt.Options{ForbidReserialize: true})
	if err != nil {
		return fmt.Errorf("proxyroute.removeCodexRouteAt: parse %s: %w", path, err)
	}
	cur, _, _ := doc.Get(cfgfmt.Path{"model_providers", ProviderName, "base_url"})
	if s, _ := cur.(string); s != value {
		return fmt.Errorf("proxyroute.removeCodexRouteAt: base_url is %q, not the %q enrolment wrote", s, value)
	}
	if err := unregisterCodexEdits(doc, priorProvider); err != nil {
		return fmt.Errorf("proxyroute.removeCodexRouteAt: edit: %w", err)
	}
	if err := writeFileKeepMode(path, doc.Bytes()); err != nil {
		return fmt.Errorf("proxyroute.removeCodexRouteAt: write: %w", err)
	}
	return nil
}

func readCodexRoot(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("proxyroute.readCodexRoot: %w", err)
	}
	root := map[string]any{}
	if err := toml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("proxyroute.readCodexRoot: parse %s: %w", path, err)
	}
	return root, nil
}
