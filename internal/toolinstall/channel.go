package toolinstall

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// NoDefinedUninstallGuidance is the honest sentence for a channel whose
// install has no well-defined uninstall command (a vendor install script). It
// never names a removal command: guessing one (rm -rf of a path the script may
// or may not have used) could destroy data the script never created.
const NoDefinedUninstallGuidance = "This tool was installed by the vendor's own install script, which has no standard uninstall command. Remove it with the vendor's uninstaller or the steps in the vendor's documentation, then use Forget to drop this record."

// ErrNoDefinedUninstall is returned by PlanUninstall for a channel whose row
// carries guidance instead of an uninstall command.
var ErrNoDefinedUninstall = errors.New("toolinstall: channel has no defined uninstall")

// ErrUnknownChannel is returned for a channel outside the table.
var ErrUnknownChannel = errors.New("toolinstall: unknown install channel")

// ErrUnrecognizedArgv is returned when an install argv does not have the
// shape its channel row describes, so no uninstall can be derived from it
// with confidence. The caller refuses rather than guessing.
var ErrUnrecognizedArgv = errors.New("toolinstall: install argv not recognised for its channel")

// flagRule says what to do with one install-time flag when deriving the
// uninstall argv.
type flagRule struct {
	// TakesValue: the flag consumes the next argv element (`--prefix DIR`).
	TakesValue bool
	// Keep: carry the flag (and its value) onto the uninstall argv, spelled
	// as Canonical (or as written when Canonical is empty). A flag that
	// scopes WHERE the package lives (npm --global/--prefix, brew --cask)
	// must be kept, or the uninstall would target a different install.
	Keep bool
	// Canonical is the uninstall spelling of a kept flag ("-g" -> "--global").
	Canonical string
	// IsPackageID: the flag's VALUE is the package identity (winget --id).
	IsPackageID bool
}

// normalizer turns the install-time package spec into the identity the
// uninstall command takes (strips a version pin, extras, or a bucket).
type normalizer func(spec string) string

// channelRule is one row of the channel table.
type channelRule struct {
	// InstallVerb is the argv after the program that marks an install
	// (npm: ["install"], uv: ["tool","install"]).
	InstallVerb []string
	// UninstallVerb replaces it in the derived uninstall argv. Empty means
	// the channel has NO defined uninstall (Guidance applies).
	UninstallVerb []string
	// Flags is the per-flag rule set; a flag absent from the map is an
	// install-only option and is DROPPED (npm --ignore-scripts,
	// --allow-scripts=…).
	Flags map[string]flagRule
	// IDFlag, when set, is how the uninstall argv names the package
	// (winget: --id) — otherwise the package is a positional argument.
	IDFlag string
	// Trailer is appended after the package (winget: --exact, so only the
	// exact id is ever matched).
	Trailer []string
	// Normalize maps the install spec to the uninstall identity.
	Normalize normalizer
	// Guidance is the honest copy for a channel with no UninstallVerb.
	Guidance string
	// HostRuntime names the runtime (integration.RuntimeRequirement.Runtime
	// vocabulary) the installed program runs on: npm lays down a
	// `#!/usr/bin/env node` shim that runs on the host's node, so a tool's
	// grounded minimum runtime is checked before such an install. "" = the
	// channel brings its own runtime (a vendor script's native binary, a brew
	// formula's dependency) or needs none.
	HostRuntime string
}

// channelTable is THE table: one row per InstallHint.Channel in the
// integration registry's closed vocabulary. TestChannelTableCoversRegistry
// pins that every channel the registry uses has a row here, so a newly
// grounded channel cannot ship an install without an uninstall decision.
var channelTable = map[string]channelRule{
	"npm": {
		InstallVerb:   []string{"install"},
		UninstallVerb: []string{"uninstall"},
		Flags: map[string]flagRule{
			"-g":       {Keep: true, Canonical: "--global"},
			"--global": {Keep: true, Canonical: "--global"},
			"--prefix": {TakesValue: true, Keep: true},
		},
		Normalize:   npmPackageName,
		HostRuntime: integration.RuntimeNode,
	},
	"uv": {
		InstallVerb:   []string{"tool", "install"},
		UninstallVerb: []string{"tool", "uninstall"},
		Flags: map[string]flagRule{
			"--python": {TakesValue: true},
			"--from":   {TakesValue: true},
			"--with":   {TakesValue: true},
		},
		Normalize: pythonPackageName,
	},
	"brew": {
		InstallVerb:   []string{"install"},
		UninstallVerb: []string{"uninstall"},
		Flags: map[string]flagRule{
			"--cask":    {Keep: true},
			"--formula": {Keep: true},
		},
		Normalize: identity,
	},
	"winget": {
		InstallVerb:   []string{"install"},
		UninstallVerb: []string{"uninstall"},
		Flags: map[string]flagRule{
			"--id":      {TakesValue: true, IsPackageID: true},
			"-e":        {},
			"--exact":   {},
			"--source":  {TakesValue: true},
			"-s":        {TakesValue: true},
			"--version": {TakesValue: true},
			"-v":        {TakesValue: true},
			"--scope":   {TakesValue: true},
		},
		IDFlag:    "--id",
		Trailer:   []string{"--exact"},
		Normalize: identity,
	},
	"scoop": {
		InstallVerb:   []string{"install"},
		UninstallVerb: []string{"uninstall"},
		Flags: map[string]flagRule{
			"--global": {Keep: true},
			"-g":       {Keep: true, Canonical: "--global"},
		},
		Normalize: scoopAppName,
	},
	"script": {
		Guidance: NoDefinedUninstallGuidance,
	},
}

// Channels returns the channels the table knows, for coverage tests.
func Channels() []string {
	out := make([]string, 0, len(channelTable))
	for c := range channelTable {
		out = append(out, c)
	}
	return out
}

// HasDefinedUninstall reports whether channel has a well-defined uninstall
// command. Unknown channels report false.
func HasDefinedUninstall(channel string) bool {
	r, ok := channelTable[channel]
	return ok && len(r.UninstallVerb) > 0
}

// GuidanceFor returns the honest guidance for a channel with no defined
// uninstall ("" when the channel has one, or is unknown).
func GuidanceFor(channel string) string {
	r, ok := channelTable[channel]
	if !ok || len(r.UninstallVerb) > 0 {
		return ""
	}
	return r.Guidance
}

// parsed is an install argv read back through its channel row.
type parsed struct {
	program string
	kept    []string
	pkg     string
}

// parseInstall reads an install argv through its channel row.
func parseInstall(r channelRule, argv []string) (parsed, error) {
	if len(argv) < 1+len(r.InstallVerb)+1 || strings.TrimSpace(argv[0]) == "" {
		return parsed{}, ErrUnrecognizedArgv
	}
	for i, v := range r.InstallVerb {
		if argv[1+i] != v {
			return parsed{}, fmt.Errorf("%w: expected %q, got %q", ErrUnrecognizedArgv, strings.Join(r.InstallVerb, " "), argv[1+i])
		}
	}
	p := parsed{program: argv[0]}
	var positional []string
	idFromFlag := ""
	rest := argv[1+len(r.InstallVerb):]
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if !strings.HasPrefix(tok, "-") {
			positional = append(positional, tok)
			continue
		}
		name, inlineVal, hasInline := strings.Cut(tok, "=")
		rule, known := r.Flags[name]
		if !known {
			// An install-only option. A flag we do not know that takes a
			// separate value would make the NEXT token look positional, so
			// only `--x=y` / boolean spellings are safe to drop here; the
			// registry's argv uses nothing else.
			continue
		}
		val := ""
		if rule.TakesValue {
			if hasInline {
				val = inlineVal
			} else {
				if i+1 >= len(rest) {
					return parsed{}, fmt.Errorf("%w: flag %s has no value", ErrUnrecognizedArgv, name)
				}
				i++
				val = rest[i]
			}
		}
		if rule.IsPackageID {
			idFromFlag = val
			continue
		}
		if rule.Keep {
			spell := name
			if rule.Canonical != "" {
				spell = rule.Canonical
			}
			if !containsString(p.kept, spell) {
				p.kept = append(p.kept, spell)
				if rule.TakesValue {
					p.kept = append(p.kept, val)
				}
			}
		}
	}
	switch {
	case idFromFlag != "" && len(positional) == 0:
		p.pkg = idFromFlag
	case idFromFlag == "" && len(positional) == 1:
		p.pkg = positional[0]
	default:
		return parsed{}, fmt.Errorf("%w: expected exactly one package, got %d", ErrUnrecognizedArgv, len(positional)+boolInt(idFromFlag != ""))
	}
	if r.Normalize != nil {
		p.pkg = r.Normalize(p.pkg)
	}
	if strings.TrimSpace(p.pkg) == "" || strings.HasPrefix(p.pkg, "-") {
		return parsed{}, fmt.Errorf("%w: empty package identity", ErrUnrecognizedArgv)
	}
	return p, nil
}

// PackageOf returns the package identity an install argv installs on its
// channel ("" when the channel has no package notion — a script — or the
// argv is not recognised). It is what the record shows the operator.
func PackageOf(channel string, installArgv []string) string {
	r, ok := channelTable[channel]
	if !ok || len(r.UninstallVerb) == 0 {
		return ""
	}
	p, err := parseInstall(r, installArgv)
	if err != nil {
		return ""
	}
	return p.pkg
}

// UninstallPlan is the derived uninstall for one recorded install.
type UninstallPlan struct {
	// Argv is the exact command to run. argv[0] is the SAME program the
	// install ran (the recorded, possibly absolute, path).
	Argv []string
	// Display is the human command (argv[0] shown by its base name).
	Display string
	// Package is the identity being removed.
	Package string
}

// PlanUninstall derives the uninstall command for an install the dashboard
// ran, from that install's recorded argv. It refuses — never guesses — when
// the channel has no defined uninstall (ErrNoDefinedUninstall, with
// GuidanceFor carrying the honest copy), the channel is unknown, or the argv
// does not have the shape its row describes.
func PlanUninstall(channel string, installArgv []string) (UninstallPlan, error) {
	r, ok := channelTable[channel]
	if !ok {
		return UninstallPlan{}, fmt.Errorf("%w: %q", ErrUnknownChannel, channel)
	}
	if len(r.UninstallVerb) == 0 {
		return UninstallPlan{}, ErrNoDefinedUninstall
	}
	p, err := parseInstall(r, installArgv)
	if err != nil {
		return UninstallPlan{}, err
	}
	argv := make([]string, 0, 1+len(r.UninstallVerb)+len(p.kept)+3+len(r.Trailer))
	argv = append(argv, p.program)
	argv = append(argv, r.UninstallVerb...)
	argv = append(argv, p.kept...)
	if r.IDFlag != "" {
		argv = append(argv, r.IDFlag)
	}
	argv = append(argv, p.pkg)
	argv = append(argv, r.Trailer...)
	return UninstallPlan{Argv: argv, Display: DisplayArgv(argv), Package: p.pkg}, nil
}

// DisplayArgv renders an argv for the operator: the program by its base name,
// and any argument containing whitespace or quotes Go-quoted.
func DisplayArgv(argv []string) string {
	out := make([]string, len(argv))
	for i, arg := range argv {
		if i == 0 {
			arg = programBase(arg)
		}
		if arg != "" && !strings.ContainsAny(arg, " \t\r\n\"'\\") {
			out[i] = arg
			continue
		}
		out[i] = strconv.Quote(arg)
	}
	return strings.Join(out, " ")
}

// programBase strips a directory from a program path on either separator, so
// a recorded absolute `/home/u/.nvm/…/bin/npm` (or a Windows path) shows as
// "npm".
func programBase(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	return filepath.Base(p)
}

// npmPackageName strips a version / dist-tag from an npm spec:
// "opencode-ai@latest" -> "opencode-ai", "@scope/pkg@1.2" -> "@scope/pkg",
// "@scope/pkg" unchanged.
func npmPackageName(spec string) string {
	at := strings.LastIndex(spec, "@")
	if at > 0 {
		return spec[:at]
	}
	return spec
}

// pythonPackageName strips extras and a version specifier from a PEP 508
// requirement: "pkg[extra]>=1" -> "pkg", "pkg==1.2" -> "pkg", "pkg@url" ->
// "pkg".
func pythonPackageName(spec string) string {
	if i := strings.IndexAny(spec, "[=<>!~@; "); i >= 0 {
		return spec[:i]
	}
	return spec
}

// scoopAppName drops a bucket qualifier ("extras/opencode" -> "opencode");
// `scoop uninstall` takes the bare app name.
func scoopAppName(spec string) string {
	if i := strings.LastIndex(spec, "/"); i >= 0 {
		spec = spec[i+1:]
	}
	if i := strings.Index(spec, "@"); i > 0 {
		spec = spec[:i]
	}
	return spec
}

func identity(spec string) string { return spec }

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
