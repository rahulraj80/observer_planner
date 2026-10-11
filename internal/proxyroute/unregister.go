package proxyroute

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
)

// UnregisterCodex undoes RegisterCodex for THIS install: it removes the
// [model_providers.openai-observer] block and puts the top-level
// model_provider back to what it was before the write (from the provenance
// record; the key is removed when nothing was recorded), preserving every
// other key. A route that is not this install's (absent, or a base_url on
// another port or host) is left untouched and reported AlreadySet with the
// value found, never an error: the undo is best-effort cleanup, like
// UnregisterClaudeCode.
//
// As with UnregisterClaudeCode, Added=true means "removed" (or, with DryRun,
// "would remove") and AlreadySet=true means "nothing of ours to remove".
// This is the one seam both `observer uninstall` and the dashboard's route
// undo use.
func (r *Registrar) UnregisterCodex() RegistrationResult {
	dir := filepath.Join(r.opts.HomeDir, ".codex")
	path := filepath.Join(dir, "config.toml")
	want := codexBaseURL(r.opts.ProxyPort)
	res := RegistrationResult{Tool: "codex", ConfigPath: path, DryRun: r.opts.DryRun}

	// Review M3-16: the edit is surgical (cfgfmt keeps every comment, key
	// order and blank line it does not touch), takes the same settings-file
	// lock Observer's own codex hook and MCP writers take, keeps the file's
	// mode and writes through a unique temp file beside it.
	if r.opts.Lock != nil && !r.opts.DryRun {
		unlock, lerr := r.opts.Lock(path)
		if lerr != nil {
			res.Error = fmt.Errorf("proxyroute.codex.unreg: lock: %w", lerr)
			return res
		}
		defer unlock()
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		res.AlreadySet = true
		return res
	}
	if err != nil {
		res.Error = fmt.Errorf("proxyroute.codex.unreg: read: %w", err)
		return res
	}
	doc, err := cfgfmt.Parse(cfgfmt.FormatTOML, raw)
	if err != nil {
		res.Error = fmt.Errorf("proxyroute.codex.unreg: parse %s: %w", path, err)
		return res
	}
	cur, _, _ := doc.Get(cfgfmt.Path{"model_providers", ProviderName, "base_url"})
	curURL, _ := cur.(string)
	if curURL == "" {
		res.AlreadySet = true
		return res
	}
	res.BaseURL = curURL
	if curURL != want {
		res.AlreadySet = true
		return res
	}

	prior := ""
	if f, perr := loadProvenance(r.ProvenancePath()); perr == nil {
		if rec := f.Find("codex", path); rec != nil {
			prior = rec.PriorProvider
		}
	}
	res.PriorProvider = prior

	if err := unregisterCodexEdits(doc, prior); err != nil {
		res.Error = fmt.Errorf("proxyroute.codex.unreg: edit: %w", err)
		return res
	}
	res.Added = true
	if r.opts.DryRun {
		return res
	}
	if err := writeFileKeepMode(path, doc.Bytes()); err != nil {
		res.Error = fmt.Errorf("proxyroute.codex.unreg: write: %w", err)
		return res
	}
	res.ProvenanceError = r.forget("codex", path)
	return res
}

// unregisterCodexEdits removes our provider table (and model_providers when
// it is left empty) and puts model_provider back to prior (removed when no
// prior was recorded), only when it still names our provider.
func unregisterCodexEdits(doc cfgfmt.Doc, prior string) error {
	if err := deleteTOMLKey(doc, cfgfmt.Path{"model_providers", ProviderName}); err != nil {
		return err
	}
	mp, _, _ := doc.Get(cfgfmt.Path{"model_provider"})
	if s, _ := mp.(string); s != ProviderName {
		return nil
	}
	if prior != "" {
		return doc.Set(cfgfmt.Path{"model_provider"}, prior)
	}
	_, err := doc.Delete(cfgfmt.Path{"model_provider"})
	return err
}

// writeFileKeepMode replaces path atomically through a unique temp file in
// the same directory, keeping the file's current permission bits.
func writeFileKeepMode(path string, b []byte) error {
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// forget drops the provenance record for a route this registrar just
// removed, so a later Leave never mistakes a route someone sets again by
// hand for one Observer wrote. Best-effort, like note: a failure is reported
// on the result and the route removal stands.
func (r *Registrar) forget(tool, configPath string) error {
	path := r.ProvenancePath()
	f, err := loadProvenance(path)
	if err != nil {
		return err
	}
	if f.Find(tool, configPath) == nil {
		return nil
	}
	f.Drop(tool, configPath)
	return saveProvenance(path, f)
}
