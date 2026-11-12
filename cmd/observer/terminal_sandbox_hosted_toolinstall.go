//go:build !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/integration"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// The agent tool in a hosted box (live run 2026-11-11 on a SandHold node:
// the node's default image has curl but neither npm nor the agent CLI; the
// launch picked npm, the registry's first Linux channel, which exited 127,
// and with a node template set no install was tried at all, so the in-box
// launcher said "claude-code: not installed" and exited 1). Now the box is
// asked what it has before anything is chosen: the tool itself (on PATH or
// in one of the registry's probe directories under the box home), else
// npm, else the vendor's official install script fetched with curl or with
// wget; a box with none of them is refused with what it lacks and how to
// fix it. The same check runs when a template is set (an image may well
// lack the tool). Every command is the registry's own grounded argv, or
// that argv with curl's fetch of the same https address swapped for
// wget's; nothing the launch carries is put in an argv or an address, and
// the install still runs only on the internet tier (the only tier that
// reaches the vendor), after the consent the launch already required.

// hostedInstallOption is one install path the box may take.
type hostedInstallOption struct {
	// Channel is the registry channel ("npm" or "script"); Fetch names the
	// script's fetcher ("curl" or "wget", "" for npm).
	Channel string `json:"channel"`
	Fetch   string `json:"fetch,omitempty"`
	// Needs are the box commands the path runs ("a|b": either one).
	Needs   []string `json:"needs"`
	Argv    []string `json:"argv"`
	Display string   `json:"display"`
}

// hostedToolCheck is what the box is asked about the tool, and how it may
// install it (the plan's tool_check).
type hostedToolCheck struct {
	Tool string `json:"tool"`
	// Names are the tool's executable names; Dirs its probe directories,
	// relative to the box home (one "*" segment allowed).
	Names []string `json:"names"`
	Dirs  []string `json:"dirs,omitempty"`
	// Installs are the install paths in the order tried; CanInstall says
	// the tier reaches the vendor (internet only).
	Installs   []hostedInstallOption `json:"installs,omitempty"`
	CanInstall bool                  `json:"can_install,omitempty"`
	// Tier and Template are for the refusal's words.
	Tier     string `json:"tier"`
	Template string `json:"template,omitempty"`
}

// hostedProbeWordRE and hostedProbeDirRE bound what reaches the probe's
// arguments: registry names and HOME-relative directories only.
var (
	hostedProbeWordRE = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)
	hostedProbeDirRE  = regexp.MustCompile(`^[A-Za-z0-9._+*-]{1,64}(/[A-Za-z0-9._+*-]{1,64}){0,8}$`)
)

// hostedCurlScriptRE is the registry's curl-piped vendor script: the one
// https address, then the shell it is piped to and that shell's arguments.
var hostedCurlScriptRE = regexp.MustCompile(`^curl -fsSL (https://[A-Za-z0-9./_~%-]+) \| ((?:bash|sh)(?: -s -- [A-Za-z0-9 -]+)?)$`)

// hostedToolDelivery decides how the agent tool reaches the box (SBX-IMG-1,
// orchestrator default): no agent CLI is uploaded. With a template the box
// is checked for the tool at first start (and, on the internet tier, may
// install it); with none, only the internet tier can install it from its
// vendor, and any other tier is refused here as tool_unavailable_in_image
// (S11). A nil check means there is nothing to look for (no grounded
// names).
//
// rule is the vendor's template rule (Vendor.TemplateRule), named in the
// refusal so a vendor whose templates are restricted says how to make one.
func hostedToolDelivery(tool, template string, tier vocab.Tier, rule string) (*hostedToolCheck, error) {
	c := hostedToolCheckFor(tool, tier, template)
	if strings.TrimSpace(template) != "" {
		if c == nil || len(c.Names) == 0 {
			return nil, nil
		}
		return c, nil
	}
	if c != nil && c.CanInstall {
		return c, nil
	}
	if c == nil || len(c.Installs) == 0 {
		return nil, fmt.Errorf("terminal_sandbox: %s: %s has no grounded Linux install command, and no template is set; name a template that holds it in [terminal.sandbox.hosted.<provider>].template", verdictHostedTool, tool)
	}
	if rule != "" {
		return nil, fmt.Errorf("terminal_sandbox: %s: no template is set, and the %s tier cannot reach %s's vendor to install it at first start; %s", verdictHostedTool, tier, tool, rule)
	}
	return nil, fmt.Errorf("terminal_sandbox: %s: no template is set, and the %s tier cannot reach %s's vendor to install it at first start; name a template that holds it in [terminal.sandbox.hosted.<provider>].template, or use the internet tier", verdictHostedTool, tier, tool)
}

// hostedToolCheckFor builds the check from the integration registry (nil
// for a tool with no Binary row).
func hostedToolCheckFor(tool string, tier vocab.Tier, template string) *hostedToolCheck {
	spec, ok := integration.For(tool)
	if !ok || spec.Binary == nil {
		return nil
	}
	c := &hostedToolCheck{Tool: tool, Tier: string(tier), Template: strings.TrimSpace(template)}
	for _, n := range spec.Binary.Names.Unix {
		if hostedProbeWordRE.MatchString(n) {
			c.Names = append(c.Names, n)
		}
	}
	for _, d := range spec.Binary.ProbeDirs {
		if d.OS == integration.ProbeUnix && !d.Abs && d.EnvRoot == "" && hostedProbeDirRE.MatchString(d.Rel) && !strings.Contains(d.Rel, "..") {
			c.Dirs = append(c.Dirs, d.Rel)
		}
	}
	c.Installs = hostedToolInstallOptions(spec.Binary.Installs)
	c.CanInstall = tier == vocab.TierInternet && len(c.Installs) > 0
	// An npm install lands under the box user's home (ensureTool), so the
	// check looks there too.
	if slices.ContainsFunc(c.Installs, func(o hostedInstallOption) bool { return o.Channel == "npm" }) && !slices.Contains(c.Dirs, hostedNPMBinDir) {
		c.Dirs = append(c.Dirs, hostedNPMBinDir)
	}
	return c
}

// hostedNPMPrefixRel is npm's prefix for an install in the box, relative to
// the box home, and hostedNPMBinDir where its executables land: the box
// user's own, never the image's root-owned global prefix. hostedNPMCacheRel
// is npm's cache for that install, removed after it (ensureTool).
const (
	hostedNPMPrefixRel = ".local"
	hostedNPMBinDir    = hostedNPMPrefixRel + "/bin"
	hostedNPMCacheRel  = ".cache/observer-npm-install"
)

// hostedToolInstallOptions are the grounded Linux install paths that run in
// a plain Linux box, in the registry's order: npm, then each vendor script
// over curl and, when the script itself needs nothing else (its Requires is
// only the curl that fetches it), the same address over wget.
func hostedToolInstallOptions(hints []integration.InstallHint) []hostedInstallOption {
	var out, wget []hostedInstallOption
	for _, h := range hints {
		if (h.OS != "" && h.OS != "linux") || len(h.Argv) == 0 {
			continue
		}
		needs := []string{h.Argv[0]}
		for _, r := range h.Requires {
			needs = append(needs, r)
		}
		switch h.Channel {
		case "npm":
			out = append(out, hostedInstallOption{Channel: "npm", Needs: needs, Argv: append([]string(nil), h.Argv...), Display: h.Display})
		case "script":
			fetch := ""
			if strings.Contains(h.Display, "curl ") {
				fetch = "curl"
			}
			out = append(out, hostedInstallOption{Channel: "script", Fetch: fetch, Needs: needs, Argv: append([]string(nil), h.Argv...), Display: h.Display})
			if w, ok := hostedWgetVariant(h); ok {
				wget = append(wget, w)
			}
		}
	}
	return append(out, wget...)
}

// hostedWgetVariant is a curl-piped vendor script fetched with wget from
// the same https address: only for the registry's `bash -lc "curl -fsSL
// <https> | <shell>"` shape, and only when the script needs nothing beyond
// the curl that fetches it (Requires is exactly curl), so the swap changes
// the fetcher and nothing the script itself relies on.
func hostedWgetVariant(h integration.InstallHint) (hostedInstallOption, bool) {
	if len(h.Argv) != 3 || h.Argv[0] != "bash" || h.Argv[1] != "-lc" || len(h.Requires) != 1 || h.Requires[0] != "curl" {
		return hostedInstallOption{}, false
	}
	m := hostedCurlScriptRE.FindStringSubmatch(h.Argv[2])
	if m == nil {
		return hostedInstallOption{}, false
	}
	cmd := "wget -qO- " + m[1] + " | " + m[2]
	return hostedInstallOption{Channel: "script", Fetch: "wget", Needs: []string{"bash", "wget"}, Argv: []string{"bash", "-lc", cmd}, Display: cmd}, true
}

// hostedPipefail (applied when the install runs; the option keeps the
// registry's argv) makes a `bash -lc "<fetch> | <shell>"` install fail when
// the fetch fails: without pipefail the pipe returns the shell's 0 when the
// download was refused, and the install looked done (seen live on sb,
// 2026-11-11). Any other argv is returned as it is.
func hostedPipefail(argv []string) []string {
	out := append([]string(nil), argv...)
	if len(out) == 3 && out[0] == "bash" && out[1] == "-lc" && strings.Contains(out[2], "|") {
		out[2] = "set -o pipefail; " + out[2]
	}
	return out
}

// hostedToolProbeScript answers what the box has. Arguments: the tool's
// names, "--", its HOME-relative probe directories, "--", the commands the
// install paths need. It prints "tool <path>" and stops when the tool is
// there, else "has <command>" for each command present. Globbing is off
// except for a probe directory's own "*" segment, under $HOME.
const hostedToolProbeScript = `set -f
names=; dirs=; needs=; part=0
for a in "$@"; do
  if [ "$a" = "--" ]; then part=$((part+1)); continue; fi
  case $part in 0) names="$names $a";; 1) dirs="$dirs $a";; *) needs="$needs $a";; esac
done
for n in $names; do
  p=$(command -v "$n" 2>/dev/null) && [ -n "$p" ] && { echo "tool $p"; exit 0; }
done
for d in $dirs; do for n in $names; do
  set +f
  for f in "$HOME"/$d/$n; do
    if [ -f "$f" ] && [ -x "$f" ]; then echo "tool $f"; exit 0; fi
  done
  set -f
done; done
for t in $needs; do command -v "$t" >/dev/null 2>&1 && echo "has $t"; done
exit 0`

// hostedBoxSpaceLine prints "space <free KiB> <size KiB>" for the file
// system of the box home (nothing when df gives no answer).
const hostedBoxSpaceLine = `set -- $(df -Pk -- "$HOME" 2>/dev/null | tail -n 1)
[ -n "$4" ] && echo "space $4 $2"`

// hostedToolRunScript starts the tool an install just placed, once, with
// --version (live run 2026-11-12 on SandHold's Shared tier: npm placed
// Claude Code's launcher but its native binary, a platform optional
// dependency npm let fail, did not fit in the box's 512 MiB store, so the
// probe found the tool, the agent exited 1 at once and the run ended with
// no word of why). Argument: the tool's path. On a failure it prints the
// tool's own last words; then the space line; it exits with the tool's
// status.
const hostedToolRunScript = `t=$1
out=$("$t" --version 2>&1 </dev/null); c=$?
[ "$c" -eq 0 ] || printf '%s\n' "$out" | tail -c 1500
` + hostedBoxSpaceLine + `
exit "$c"`

// hostedToolRunArgv is the run check's argv for the tool at path.
func hostedToolRunArgv(path string) []string {
	return []string{"sh", "-c", hostedToolRunScript, "sh", path}
}

// hostedBoxSpaceArgv asks the box how much room its home has.
func hostedBoxSpaceArgv() []string {
	return []string{"sh", "-c", hostedBoxSpaceLine + "\nexit 0"}
}

// hostedSpace is the box home's file system: free and total KiB (zero
// when the box did not say).
type hostedSpace struct{ FreeKiB, SizeKiB int64 }

// parseHostedToolRun splits the run check's output into the tool's words
// and the space line.
func parseHostedToolRun(out []byte) (string, hostedSpace) {
	var words []string
	var sp hostedSpace
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if f := strings.Fields(line); len(f) == 3 && f[0] == "space" {
			free, e1 := strconv.ParseInt(f[1], 10, 64)
			size, e2 := strconv.ParseInt(f[2], 10, 64)
			if e1 == nil && e2 == nil && free >= 0 && size > 0 {
				sp = hostedSpace{free, size}
				continue
			}
		}
		words = append(words, line)
	}
	return hostedLastLine(strings.Join(words, "\n")), sp
}

// hostedLowSpaceKiB is the free room under which a failed install is put
// down to the box's store being full.
const hostedLowSpaceKiB = 64 << 10

// hostedNoSpaceRE finds a write that ran out of room in an install's
// output (npm: "TAR_ENTRY_ERROR ENOSPC: no space left on device"). npm lets
// an optional dependency fail and exits 0, then removes what it wrote, so
// by the time the box is asked its store may look roomy again (live run
// 2026-11-12: 332 MiB free of 512 MiB after Claude Code's 326 MiB native
// binary did not fit).
var hostedNoSpaceRE = regexp.MustCompile(`(?i)\bENOSPC\b|no space left on device`)

// note is the space in words for a refusal ("" when unknown and the
// install said nothing of room); a store that ran out, or is nearly full,
// is named as the likely cause, with the fix.
func (s hostedSpace) note(installOut string) string {
	full := hostedNoSpaceRE.MatchString(installOut)
	n := ""
	if s.SizeKiB > 0 {
		n = fmt.Sprintf("the box home's store has %d MiB free of %d MiB", s.FreeKiB>>10, s.SizeKiB>>10)
	}
	switch {
	case full:
		if n != "" {
			n = " (" + n + " now that the install's leftovers are gone)"
		}
		return "the install ran out of room in the box: it reported no space left on device" + n + "; use a box with more storage, or a template whose image already holds the tool"
	case n != "" && s.FreeKiB < hostedLowSpaceKiB:
		return n + "; the install most likely ran out of room (a package manager can leave a tool that cannot start when a part of it does not fit): use a box with more storage, or a template whose image already holds the tool"
	}
	return n
}

// hostedToolProbeArgv is the probe's argv for c.
func hostedToolProbeArgv(c hostedToolCheck) []string {
	argv := append([]string{"sh", "-c", hostedToolProbeScript, "sh"}, c.Names...)
	argv = append(append(argv, "--"), c.Dirs...)
	argv = append(argv, "--")
	seen := map[string]bool{}
	for _, o := range c.Installs {
		for _, n := range o.Needs {
			for _, alt := range strings.Split(n, "|") {
				if hostedProbeWordRE.MatchString(alt) && !seen[alt] {
					seen[alt] = true
					argv = append(argv, alt)
				}
			}
		}
	}
	return argv
}

// hostedBoxTools is the probe's answer.
type hostedBoxTools struct {
	// Tool is where the tool is ("" when not found).
	Tool string
	Has  map[string]bool
}

// parseHostedToolProbe reads the probe's output.
func parseHostedToolProbe(out []byte) hostedBoxTools {
	got := hostedBoxTools{Has: map[string]bool{}}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "tool "):
			got.Tool = strings.TrimSpace(strings.TrimPrefix(line, "tool "))
		case strings.HasPrefix(line, "has "):
			got.Has[strings.TrimSpace(strings.TrimPrefix(line, "has "))] = true
		}
	}
	return got
}

// missing are the needs of o the box lacks.
func (b hostedBoxTools) missing(o hostedInstallOption) []string {
	var miss []string
	for _, n := range o.Needs {
		ok := false
		for _, alt := range strings.Split(n, "|") {
			if b.Has[alt] {
				ok = true
				break
			}
		}
		if !ok {
			miss = append(miss, strings.ReplaceAll(n, "|", " or "))
		}
	}
	return miss
}

// hostedChooseInstall picks how the tool reaches the box: nothing when it
// is there, else the first install path whose commands the box has, else a
// refusal naming what the box lacks and how to fix it.
func hostedChooseInstall(c hostedToolCheck, got hostedBoxTools) (*hostedInstallOption, error) {
	if got.Tool != "" {
		return nil, nil
	}
	image := "the box"
	if c.Template != "" {
		image = "the box (template " + c.Template + ")"
	}
	fix := "use a box image (a template in [terminal.sandbox.hosted.<provider>].template) that has " + c.Tool
	if !c.CanInstall {
		if len(c.Installs) == 0 {
			return nil, fmt.Errorf("%s: %s is not installed in %s, and it has no grounded Linux install command; %s", verdictHostedTool, c.Tool, image, fix)
		}
		return nil, fmt.Errorf("%s: %s is not installed in %s, and the %s tier cannot reach its vendor to install it; %s, or use the internet tier", verdictHostedTool, c.Tool, image, c.Tier, fix)
	}
	var lacks []string
	for i := range c.Installs {
		o := c.Installs[i]
		miss := got.missing(o)
		if len(miss) == 0 {
			return &o, nil
		}
		for _, m := range miss {
			if !slices.Contains(lacks, m) {
				lacks = append(lacks, m)
			}
		}
	}
	return nil, fmt.Errorf("%s: %s is not installed in %s, and the box cannot install it (it lacks %s); %s, or one with npm or curl", verdictHostedTool, c.Tool, image, hostedAndList(lacks), fix)
}

// hostedAndList is "a", "a and b", "a, b and c".
func hostedAndList(xs []string) string {
	if len(xs) < 2 {
		return strings.Join(xs, "")
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

// hostedLastLine is s cut to its first and last non-empty lines, for a
// bounded refusal (an install's output can run to many lines).
func hostedLastLine(s string) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	switch len(lines) {
	case 0:
		return ""
	case 1:
		return lines[0]
	}
	return lines[0] + " ... " + lines[len(lines)-1]
}

// hostedWithNote is "; " and the note, or "" for none.
func hostedWithNote(n string) string {
	if n == "" {
		return ""
	}
	return "; " + n
}
