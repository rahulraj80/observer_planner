package guard

import (
	"os"
	"path/filepath"
)

// resolveExistingLinks is the policy engine's injected symlink resolver
// (policy.Config.ResolveLinks, Sol A2-04): it resolves the symlinks in the
// longest EXISTING prefix of p and re-appends the rest (a glob segment such
// as "*" never exists, so `out/*` resolves `out` and keeps `*`). p is the
// engine's normalized forward-slash form. It returns "" when p is not an
// absolute path on THIS machine's flavor (a Windows-shaped path on a Linux
// daemon and vice versa: the cross-OS case stays lexical), when no prefix
// exists, or when resolution fails.
func resolveExistingLinks(p string) string {
	if p == "" {
		return ""
	}
	native := filepath.FromSlash(p)
	if !filepath.IsAbs(native) {
		return ""
	}
	cur := filepath.Clean(native)
	var rest []string
	for {
		if _, err := os.Lstat(cur); err == nil {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
	real, err := filepath.EvalSymlinks(cur)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(filepath.Join(append([]string{real}, rest...)...))
}
