package agentconfig

import (
	"strings"
)

// Path resolution (phase P4): a FileRow's per-OS path, with its tokens
// expanded for one machine. Pure: the caller supplies the OS, home, project
// root and environment; nothing here touches the file system.

// PathEnv is the machine a path is resolved for.
type PathEnv struct {
	// GOOS is "linux", "darwin" or "windows" (anything else resolves as
	// linux).
	GOOS string
	// Home is the user's home directory (absolute).
	Home string
	// Project is the project root, "" when no project is selected.
	Project string
	// Getenv reads an environment variable; nil reads nothing.
	Getenv func(string) string
	// SystemRoot, when set, is prefixed to every absolute path that is not
	// under Home or Project (managed / system files). Tests set it so a
	// resolution never names the real /etc or C:\Program Files.
	SystemRoot string
}

// PathStatus says whether a row resolved to one concrete file.
type PathStatus string

// The closed PathStatus vocabulary.
const (
	// PathResolved: Path names one concrete file.
	PathResolved PathStatus = "resolved"
	// PathNoOS: the tool has no such file on this OS.
	PathNoOS PathStatus = "no_os"
	// PathNeedsProject: a project-scope row with no project selected.
	PathNeedsProject PathStatus = "needs_project"
	// PathDirectory: the vendor documents a directory, not a file name.
	PathDirectory PathStatus = "directory"
	// PathGlob: the row is a glob (instruction / skill / agent rows); the
	// boundary lists its matches.
	PathGlob PathStatus = "glob"
)

// ResolvedPath is one row's resolution.
type ResolvedPath struct {
	Status PathStatus
	// Path is the expanded path (also set for PathGlob and PathDirectory,
	// expanded as far as the tokens go).
	Path string
}

func (e PathEnv) windows() bool { return e.GOOS == "windows" }

func (e PathEnv) env(name string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(name)
}

// rawPath picks the row's path for the env's OS.
func (e PathEnv) rawPath(p OSPaths) string {
	switch e.GOOS {
	case "windows":
		return p.Windows
	case "darwin":
		return p.Darwin
	}
	return p.Linux
}

// Join joins segments with the env's separator.
func (e PathEnv) Join(parts ...string) string {
	sep := "/"
	if e.windows() {
		sep = `\`
	}
	var b strings.Builder
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i > 0 && b.Len() > 0 {
			s := b.String()
			if !strings.HasSuffix(s, "/") && !strings.HasSuffix(s, `\`) {
				b.WriteString(sep)
			}
			p = strings.TrimLeft(p, `/\`)
		}
		b.WriteString(p)
	}
	return e.native(b.String())
}

// native converts separators to the env's OS.
func (e PathEnv) native(p string) string {
	if e.windows() {
		return strings.ReplaceAll(p, "/", `\`)
	}
	return strings.ReplaceAll(p, `\`, "/")
}

// tokenRule expands one leading path token.
type tokenRule struct {
	Token string
	// Expand returns the replacement for the token; "" leaves the path
	// unresolvable.
	Expand func(e PathEnv) string
}

// pathTokens is the closed token table (OSPaths doc comment).
var pathTokens = []tokenRule{
	{"<project>", func(e PathEnv) string { return e.Project }},
	{"~", func(e PathEnv) string { return e.Home }},
	{"%USERPROFILE%", func(e PathEnv) string { return e.Home }},
	{"%APPDATA%", func(e PathEnv) string {
		if v := e.env("APPDATA"); v != "" {
			return v
		}
		return e.Join(e.Home, "AppData", "Roaming")
	}},
	{"%LOCALAPPDATA%", func(e PathEnv) string {
		if v := e.env("LOCALAPPDATA"); v != "" {
			return v
		}
		return e.Join(e.Home, "AppData", "Local")
	}},
	{"%ProgramData%", func(e PathEnv) string {
		if v := e.env("ProgramData"); v != "" {
			return e.system(v)
		}
		return e.system(`C:\ProgramData`)
	}},
	{"%ProgramFiles%", func(e PathEnv) string {
		if v := e.env("ProgramFiles"); v != "" {
			return e.system(v)
		}
		return e.system(`C:\Program Files`)
	}},
	{"$XDG_CONFIG_HOME", func(e PathEnv) string {
		if v := e.env("XDG_CONFIG_HOME"); v != "" {
			return v
		}
		return e.Join(e.Home, ".config")
	}},
	{"$XDG_DATA_HOME", func(e PathEnv) string {
		if v := e.env("XDG_DATA_HOME"); v != "" {
			return v
		}
		return e.Join(e.Home, ".local", "share")
	}},
}

// system re-roots an absolute system path under SystemRoot when set.
func (e PathEnv) system(p string) string {
	if e.SystemRoot == "" {
		return p
	}
	rel := p
	if len(rel) >= 2 && rel[1] == ':' { // drive letter
		rel = rel[2:]
	}
	return e.Join(e.SystemRoot, rel)
}

// ResolvePath expands a row's path for the env.
func ResolvePath(f FileRow, e PathEnv) ResolvedPath {
	raw := e.rawPath(f.Paths)
	if raw == "" {
		return ResolvedPath{Status: PathNoOS}
	}
	expanded, ok := e.expand(raw)
	if !ok {
		if strings.HasPrefix(raw, "<project>") {
			return ResolvedPath{Status: PathNeedsProject}
		}
		return ResolvedPath{Status: PathNoOS}
	}
	switch {
	case strings.ContainsAny(raw, "*?["):
		return ResolvedPath{Status: PathGlob, Path: expanded}
	case strings.HasSuffix(raw, "/") || strings.HasSuffix(raw, `\`):
		return ResolvedPath{Status: PathDirectory, Path: expanded}
	}
	return ResolvedPath{Status: PathResolved, Path: expanded}
}

// expand replaces the leading token (if any). A path with no token that is
// absolute is a system path (re-rooted under SystemRoot); a relative path
// with no token is project-relative (guidance rows store "CLAUDE.md").
func (e PathEnv) expand(raw string) (string, bool) {
	for _, t := range pathTokens {
		if raw == t.Token || strings.HasPrefix(raw, t.Token+"/") || strings.HasPrefix(raw, t.Token+`\`) {
			base := t.Expand(e)
			if base == "" {
				return "", false
			}
			rest := strings.TrimPrefix(raw, t.Token)
			return e.Join(base, rest), true
		}
	}
	if strings.HasPrefix(raw, "/") || (len(raw) >= 3 && raw[1] == ':' && (raw[2] == '\\' || raw[2] == '/')) {
		return e.native(e.system(raw)), true
	}
	if e.Project == "" {
		return "", false
	}
	return e.Join(e.Project, raw), true
}

// MatchGlob reports whether a slash-separated relative path matches a
// guidance-style glob: "*" matches within one segment, "**" matches zero or
// more segments, "?" one character. Both inputs use "/" separators.
func MatchGlob(pattern, rel string) bool {
	return matchSegs(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

func matchSegs(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			if len(pat) == 1 {
				return true
			}
			for i := 0; i <= len(segs); i++ {
				if matchSegs(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 || !matchSeg(pat[0], segs[0]) {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// matchSeg matches one segment with * and ? wildcards.
func matchSeg(p, s string) bool {
	if p == "" {
		return s == ""
	}
	switch p[0] {
	case '*':
		for i := 0; i <= len(s); i++ {
			if matchSeg(p[1:], s[i:]) {
				return true
			}
		}
		return false
	case '?':
		return s != "" && matchSeg(p[1:], s[1:])
	}
	return s != "" && s[0] == p[0] && matchSeg(p[1:], s[1:])
}
