package hook

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/marmutapp/superbased-observer/internal/hook/commandcodemod"
)

// registerCommandCode drops the go:embed'd observer-guard.ts bridge
// into ~/.commandcode/mods/ (Part B item 2, phase-3a: new
// HookCommandCodeMod mechanism — internal/hook/commandcodemod).
// Unlike every other registrar in this file, this is NOT a
// JSON/YAML config-file mutation: commandcode's Mods SDK discovers
// loose .ts files by directory presence, so "registering" means
// writing (or overwriting, on re-run) the one file. No lock/pinned-
// target machinery applies — there is no shared config file another
// process could be racing to write.
//
// FIXED (FIX cluster, item 5): this used to unconditionally overwrite
// whatever (if anything) already sat at the mod path, with no
// conflict guard and no checksum recorded — the only writer in this
// file without the takeover-refusal / --force / checksum discipline
// every JSON/YAML writer above has. Now: an existing file that
// doesn't look observer-written (commandcodemod.LooksLikeObserverPlugin)
// is refused without --force (mirroring registerCascade's own
// foreign-entry guard), backed up to path+".bak" before being
// overwritten under --force (mirroring writeYAMLMap's backup
// discipline for a lossy takeover), and a checksum is recorded after
// every successful write so unregisterCommandCode can detect
// out-of-band drift before deleting.
func (r *Registry) registerCommandCode() RegistrationResult {
	res := RegistrationResult{Tool: "command-code", DryRun: r.opts.DryRun}
	dir := filepath.Join(r.opts.HomeDir, ".commandcode", "mods")
	path := filepath.Join(dir, commandcodemod.ModFileName)
	res.ConfigPath = path

	existing, readErr := os.ReadFile(path)
	alreadyInstalled := readErr == nil
	foreign := alreadyInstalled && !commandcodemod.LooksLikeObserverPlugin(existing)
	if foreign && !r.opts.Force {
		res.Error = fmt.Errorf("hook.registerCommandCode: %s already exists and doesn't look like an observer-managed file; pass --force to overwrite", path)
		return res
	}

	events := commandcodemod.EventsExcept(func(e string) bool { return r.skipEvent("command-code", e) })
	names := commandcodemod.NamesOf(events)
	if r.opts.DryRun {
		if !alreadyInstalled {
			res.HooksAdded = names
		} else {
			res.AlreadySet = names
		}
		return res
	}
	if foreign {
		if err := backupForeignCommandCodeMod(path, existing); err != nil {
			res.Error = fmt.Errorf("hook.registerCommandCode: %w", err)
			return res
		}
	}
	if err := commandcodemod.WritePluginEvents(dir, r.opts.BinaryPath, r.opts.ConfigPath, r.observerConfigDir(), events); err != nil {
		res.Error = fmt.Errorf("hook.registerCommandCode: %w", err)
		return res
	}
	if err := r.recordChecksum(path); err != nil {
		res.Error = fmt.Errorf("hook.registerCommandCode: %w", err)
		return res
	}
	if alreadyInstalled {
		res.AlreadySet = names
	} else {
		res.HooksAdded = names
	}
	return res
}

// observerConfigDir is the directory of the global observer config the
// registered hooks use (guard-posture.json and guard-hookfail.jsonl live
// beside it): the --config path's directory, else <home>/.observer.
func (r *Registry) observerConfigDir() string {
	if r.opts.ConfigPath != "" {
		return filepath.Dir(r.opts.ConfigPath)
	}
	if r.opts.HomeDir != "" {
		return filepath.Join(r.opts.HomeDir, ".observer")
	}
	return ""
}

// backupForeignCommandCodeMod writes a path+".bak" copy of data (the
// pre-overwrite content) — the same discipline writeYAMLMap already
// applies to a Poolside/Hermes settings.yaml takeover, adapted for a
// bare-file (non-JSON/YAML) target. Only called when registerCommandCode
// is about to overwrite a foreign (non-observer-written) file under
// --force, so an operator who had something else at this path can
// still recover it.
func backupForeignCommandCodeMod(path string, data []byte) error {
	info, err := os.Stat(path)
	mode := os.FileMode(0o600)
	if err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(path+".bak", data, mode); err != nil { //nolint:gosec // G703: the operator's own command-code config path
		return fmt.Errorf("backup %s: %w", path, err)
	}
	return nil
}

// unregisterCommandCode removes the observer-guard.ts mod file.
//
// FIXED (FIX cluster, item 5): this used to delete purely on
// commandcodemod.Installed's presence check, with no verification
// that the on-disk file was still the one observer wrote (vs. having
// been modified since, or — now that registerCommandCode can take
// over a foreign file under --force — legitimately being someone
// else's file). Now checksum-guarded exactly like
// unregisterClaudeCode: a mismatch refuses without --force.
func (r *Registry) unregisterCommandCode() UnregistrationResult {
	res := UnregistrationResult{Tool: "command-code", DryRun: r.opts.DryRun}
	dir := filepath.Join(r.opts.HomeDir, ".commandcode", "mods")
	path := filepath.Join(dir, commandcodemod.ModFileName)
	res.ConfigPath = path

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return res
		}
		res.Error = fmt.Errorf("hook.unregisterCommandCode: read: %w", err)
		return res
	}
	if r.opts.DryRun {
		res.HooksRemoved = commandcodemod.EventNames()
		return res
	}

	match, err := r.checksumMatches(path, raw)
	if err != nil {
		res.Error = fmt.Errorf("hook.unregisterCommandCode: checksum: %w", err)
		return res
	}
	res.ChecksumMatch = match
	if !match && !r.opts.Force {
		res.Error = fmt.Errorf("hook.unregisterCommandCode: %s has been modified since install (checksum mismatch); pass --force to remove anyway", path)
		return res
	}

	if err := commandcodemod.RemovePlugin(dir); err != nil {
		res.Error = fmt.Errorf("hook.unregisterCommandCode: %w", err)
		return res
	}
	if err := r.removeChecksum(path); err != nil {
		res.Error = fmt.Errorf("hook.unregisterCommandCode: %w", err)
		return res
	}
	res.HooksRemoved = commandcodemod.EventNames()
	return res
}
