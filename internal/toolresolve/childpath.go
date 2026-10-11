package toolresolve

import "path/filepath"

// childpath.go answers "which interpreter will the launched child actually
// run on?", the question a minimum-runtime check needs (a `#!/usr/bin/env
// node` shim runs on the FIRST node its PATH resolves, and that PATH is the
// launcher's composed one, not the daemon's). It mirrors the composition
// cmd/observer's childPATH applies to every spawn (DI-04b): the resolved
// binary's own directory, then the login-only merged-PATH dirs, then the
// process PATH. cmd/observer pins the two orders equal
// (TestChildPATHMatchesToolresolveOrder), so the check can never look at a
// different interpreter than the launch uses.

// ChildPathDirs returns the absolute directories, in priority order, of the
// PATH a launcher hands a child whose binary lives in binDir ("" for none).
// Relative and empty entries are dropped and duplicates keep their first
// position, as the launcher's composition does.
func ChildPathDirs(env Env, binDir string) []string {
	var discard []string
	merged := mergePath(env, &discard)
	ordered := []string{binDir}
	for _, e := range merged {
		if e.login {
			ordered = append(ordered, e.dir)
		}
	}
	for _, e := range merged {
		if !e.login {
			ordered = append(ordered, e.dir)
		}
	}
	out := make([]string, 0, len(ordered))
	seen := map[string]bool{}
	for _, d := range ordered {
		if d == "" {
			continue
		}
		c := filepath.Clean(d)
		if !filepath.IsAbs(c) || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

// FindInChildPath returns the first regular, executable file named program
// on ChildPathDirs(env, binDir), or "" when none resolves (or program is
// empty). Pure: every probe goes through env.
func FindInChildPath(env Env, binDir, program string) string {
	if program == "" {
		return ""
	}
	for _, dir := range ChildPathDirs(env, binDir) {
		if c, ok := statCandidate(env, filepath.Join(dir, program), OriginProcessPath); ok {
			return c.Path
		}
	}
	return ""
}

// ChildInterpreterPath returns the interpreter res.Bin's `#!/usr/bin/env
// <name>` shebang will run on when the launcher spawns it, or "" when res
// names no such interpreter (a native binary, an absolute shebang) or none
// resolves.
func ChildInterpreterPath(res Resolution, env Env) string {
	if res.Interpreter == "" || res.Bin == "" {
		return ""
	}
	return FindInChildPath(env, filepath.Dir(res.Bin), res.Interpreter)
}
