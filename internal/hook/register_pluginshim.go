package hook

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/marmutapp/superbased-observer/internal/guardposture"
	"github.com/marmutapp/superbased-observer/internal/hook/pluginshim"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// Plugin-shim format writer (hook-parity Lane D): place the ONE rendered
// TypeScript shim (internal/hook/pluginshim) into an in-process plugin
// host's plugin directory. Selected by the HookPluginShimTS mechanism, never
// a tool name: the host row (pluginshim.HostFor) supplies the directory and
// the plugin contract, and the registry row's Registered plugin bindings
// supply the subscribed events. Like registerCommandCode this is a loose
// file the host discovers by directory presence, so "registering" is
// writing (or refreshing) that one file under the same conflict guard,
// backup and checksum discipline.

// pluginShimDialects are the dialects a shim-subscribed binding speaks.
var pluginShimDialects = map[string]bool{PluginDialectToolGate: true, PromptDialectPluginShim: true}

// pluginShimEvents maps every Registered plugin-dialect binding of c to its
// capability (the shim's event map), plus the sorted event list.
func pluginShimEvents(c integration.Capability) (map[string]string, []string) {
	events := map[string]string{}
	for _, b := range c.Hook.Bindings {
		if b.Registered && pluginShimDialects[b.Dialect] {
			if _, seen := events[b.NativeEvent]; !seen {
				events[b.NativeEvent] = string(b.Cap)
			}
		}
	}
	names := make([]string, 0, len(events))
	for e := range events {
		names = append(names, e)
	}
	sort.Strings(names)
	return events, names
}

// pluginShimTarget resolves the host row and the shim's file path.
func (r *Registry) pluginShimTarget(tool string) (pluginshim.Host, string, error) {
	h, ok := pluginshim.HostFor(tool)
	if !ok {
		return pluginshim.Host{}, "", fmt.Errorf("no plugin-shim host row for %q", tool)
	}
	dir, err := h.Dir(r.opts.HomeDir, r.opts.Getenv)
	if err != nil {
		return h, "", err
	}
	return h, filepath.Join(dir, h.File()), nil
}

// pluginShimRenderOptions are the install-time values baked into the shim.
func (r *Registry) pluginShimRenderOptions(events map[string]string) pluginshim.RenderOptions {
	cfg := r.opts.ConfigPath
	if cfg == "" && r.opts.HomeDir != "" {
		cfg = filepath.Join(r.opts.HomeDir, ".observer", "config.toml")
	}
	return pluginshim.RenderOptions{
		Events: events, Bin: r.opts.BinaryPath, Config: r.opts.ConfigPath,
		PosturePath: guardposture.PosturePath(cfg), SpoolPath: guardposture.SpoolPath(cfg),
		PostureSchema: guardposture.Schema,
		// T-050 / review M2-02: the shim reads the posture only while it is
		// fresh, with the same window and config-mtime rule as the Go hook.
		ConfigFile:      cfg,
		PostureMaxAgeMS: int(guardposture.MaxAge.Milliseconds()),
		PostureSkewMS:   int(guardposture.MaxFutureSkew.Milliseconds()),
	}
}

// registerPluginShim writes the rendered shim for c's host.
func (r *Registry) registerPluginShim(c integration.Capability) RegistrationResult {
	res := RegistrationResult{Tool: c.Tool, DryRun: r.opts.DryRun}
	h, path, err := r.pluginShimTarget(c.Tool)
	if err != nil {
		res.Error = fmt.Errorf("hook.registerPluginShim: %w", err)
		return res
	}
	res.ConfigPath = path
	events, names := pluginShimEvents(c)
	if len(names) == 0 {
		res.Error = fmt.Errorf("hook.registerPluginShim: %s registers no plugin events", c.Tool)
		return res
	}
	// Options.SkipEvent (observer start's owes-a-live-test filter) drops a
	// subscribed event from the one shim file, as every other writer does.
	kept := names[:0:0]
	for _, e := range names {
		if r.skipEvent(c.Tool, e) {
			delete(events, e)
			continue
		}
		kept = append(kept, e)
	}
	names = kept
	if len(names) == 0 {
		res.Skipped = true
		res.SkipKind = SkipNotApplicable
		res.SkipReason = "every subscribed plugin event still owes a live test"
		return res
	}

	existing, readErr := os.ReadFile(path)
	alreadyInstalled := readErr == nil
	foreign := alreadyInstalled && !pluginshim.LooksLikeObserverShim(existing)
	if foreign && !r.opts.Force {
		res.Error = fmt.Errorf("hook.registerPluginShim: %s already exists and doesn't look like an observer-managed file; pass --force to overwrite", path)
		return res
	}
	// Currency is BYTE-exact (review M2-07): the marker only says the file
	// is Observer's, not that it is the shim Observer renders today. A shim
	// with an older binary path, another event set, an older template or a
	// hand edit reads as not registered, exactly like a stale command in a
	// JSON settings file.
	want, err := pluginshim.Render(h, r.pluginShimRenderOptions(events))
	if err != nil {
		res.Error = fmt.Errorf("hook.registerPluginShim: %w", err)
		return res
	}
	current := alreadyInstalled && !foreign && bytes.Equal(existing, want)
	if r.opts.DryRun {
		if current {
			res.AlreadySet = names
		} else {
			res.HooksAdded = names
		}
		return res
	}
	if foreign {
		if err := backupForeignCommandCodeMod(path, existing); err != nil {
			res.Error = fmt.Errorf("hook.registerPluginShim: %w", err)
			return res
		}
	}
	if _, err := pluginshim.Write(filepath.Dir(path), h, r.pluginShimRenderOptions(events)); err != nil {
		res.Error = fmt.Errorf("hook.registerPluginShim: %w", err)
		return res
	}
	if err := r.recordChecksum(path); err != nil {
		res.Error = fmt.Errorf("hook.registerPluginShim: %w", err)
		return res
	}
	if current {
		res.AlreadySet = names
	} else {
		res.HooksAdded = names
	}
	return res
}

// unregisterPluginShim removes the shim, checksum-guarded like every other
// unregister: a file modified since install is refused without --force.
func (r *Registry) unregisterPluginShim(c integration.Capability) UnregistrationResult {
	res := UnregistrationResult{Tool: c.Tool, DryRun: r.opts.DryRun}
	h, path, err := r.pluginShimTarget(c.Tool)
	if errors.Is(err, pluginshim.ErrNoInstallLocation) {
		// A host with no install location never received a shim: nothing
		// to remove (T-732).
		return res
	}
	if err != nil {
		res.Error = fmt.Errorf("hook.unregisterPluginShim: %w", err)
		return res
	}
	res.ConfigPath = path
	_, names := pluginShimEvents(c)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return res
		}
		res.Error = fmt.Errorf("hook.unregisterPluginShim: read: %w", err)
		return res
	}
	if !pluginshim.LooksLikeObserverShim(raw) && !r.opts.Force {
		res.Error = fmt.Errorf("hook.unregisterPluginShim: %s is not an observer-managed file; pass --force to remove anyway", path)
		return res
	}
	if r.opts.DryRun {
		res.HooksRemoved = names
		return res
	}
	match, err := r.checksumMatches(path, raw)
	if err != nil {
		res.Error = fmt.Errorf("hook.unregisterPluginShim: checksum: %w", err)
		return res
	}
	res.ChecksumMatch = match
	if !match && !r.opts.Force {
		res.Error = fmt.Errorf("hook.unregisterPluginShim: %s has been modified since install (checksum mismatch); pass --force to remove anyway", path)
		return res
	}
	if err := pluginshim.Remove(filepath.Dir(path), h); err != nil {
		res.Error = fmt.Errorf("hook.unregisterPluginShim: %w", err)
		return res
	}
	if err := r.removeChecksum(path); err != nil {
		res.Error = fmt.Errorf("hook.unregisterPluginShim: %w", err)
		return res
	}
	res.HooksRemoved = names
	return res
}

// installedPluginShimHosts returns every plugin-shim host (a registry row
// whose mechanism is HookPluginShimTS and whose host row has a grounded
// location) whose vendor config directory, the shim directory's parent,
// exists. Sorted (pluginshim.Tools order).
func (r *Registry) installedPluginShimHosts() []string {
	var out []string
	for _, t := range pluginshim.Tools() {
		if _, ok := pluginShimCapability(t); !ok {
			continue
		}
		h, _ := pluginshim.HostFor(t)
		if !h.Writable() {
			continue
		}
		dir, err := h.Dir(r.opts.HomeDir, r.opts.Getenv)
		if err == nil && r.dirExists(filepath.Dir(dir)) {
			out = append(out, t)
		}
	}
	return out
}

// pluginShimCapability returns tool's registry row when its mechanism is the
// plugin shim (the dispatch Register / Unregister use: capability shape, not
// a tool name).
func pluginShimCapability(tool string) (integration.Capability, bool) {
	c, ok := integration.For(tool)
	if !ok || c.Hook.Mechanism != integration.HookPluginShimTS {
		return integration.Capability{}, false
	}
	return c, true
}
