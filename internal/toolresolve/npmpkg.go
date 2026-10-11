package toolresolve

import (
	"path/filepath"
	"strings"
)

// NPMPackageRoot returns the root directory of the npm package a resolved
// binary belongs to: the `node_modules/<name>` (or `node_modules/@scope/<name>`)
// directory that contains the file bin really is, after env.EvalSymlinks (an
// npm global shim is a symlink into the package, e.g. ~/.local/bin/gemini ->
// ../lib/node_modules/@google/gemini-cli/bundle/gemini.js). The LAST
// node_modules segment wins, so a binary of a nested dependency maps to that
// dependency. "" when bin does not live inside a node_modules package. Pure:
// the only probe is env.EvalSymlinks (nil = take bin as is).
func NPMPackageRoot(env Env, bin string) string {
	if strings.TrimSpace(bin) == "" {
		return ""
	}
	real := bin
	if env.EvalSymlinks != nil {
		if r, err := env.EvalSymlinks(bin); err == nil && r != "" {
			real = r
		}
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(real)), "/")
	nm := -1
	for i, part := range parts {
		if part == "node_modules" {
			nm = i
		}
	}
	if nm < 0 || nm+1 >= len(parts) {
		return ""
	}
	end := nm + 2 // exclusive: node_modules/<name>
	if strings.HasPrefix(parts[nm+1], "@") {
		end = nm + 3 // node_modules/@scope/<name>
	}
	// The binary must be a file INSIDE the package, not the package dir.
	if end >= len(parts) || parts[end-1] == "" {
		return ""
	}
	return filepath.FromSlash(strings.Join(parts[:end], "/"))
}
