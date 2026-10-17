package main

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/installshape"
)

// registration.go is the ONE renderer of what a hook or MCP entry invokes
// (lane FIX-UPGRADE U1 + U5). `observer init`, the interactive init, the
// enrolment's --wire-clients step and `observer start`'s auto-registration
// all build their hook / MCP options through it, so the two writers stop
// rewriting each other's entries:
//
//   - the command is installshape.Decide over the running binary: the
//     package manager's launcher when the binary lives in an npm or pip
//     tree (which an upgrade deletes), the real path otherwise (U1);
//   - the --config argument is written only when it names a config other
//     than the default ~/.observer/config.toml, which a hook or MCP server
//     reads anyway (U5). init used to omit it and auto-register used to
//     always spell it out, so each rewrote the other.

// registrationCommand is the command hook and MCP entries invoke for the
// observer binary at binary (an absolute, symlink-resolved path).
func registrationCommand(binary string) string {
	return registrationDecision(binary).Command
}

// registrationDecision is installshape.Decide with the file system and PATH
// as its inputs.
func registrationDecision(binary string) installshape.Decision {
	return installshape.Decide(installshape.Input{
		GOOS:           runtime.GOOS,
		Binary:         binary,
		PathCandidates: pathObserverCandidates(os.Getenv("PATH")),
		Resolve:        resolveRegularFile,
	})
}

// pathObserverCandidates lists every `observer` launcher on PATH, in order.
func pathObserverCandidates(pathEnv string) []string {
	names := []string{"observer"}
	if runtime.GOOS == "windows" {
		names = []string{"observer.exe"}
	}
	var out []string
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		for _, n := range names {
			out = append(out, filepath.Join(dir, n))
		}
	}
	return out
}

// resolveRegularFile follows symlinks and reports whether p ends at a
// regular file.
func resolveRegularFile(p string) (string, bool) {
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", false
	}
	abs, err := filepath.Abs(r)
	if err != nil {
		return "", false
	}
	fi, err := os.Stat(abs)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	return abs, true
}

// registrationConfigArg is the --config value a hook or MCP entry carries:
// "" when configPath is empty or names the default config file for homeDir
// ("" = the real home), configPath otherwise.
func registrationConfigArg(configPath, homeDir string) string {
	if configPath == "" {
		return ""
	}
	def := ""
	if homeDir != "" {
		def = filepath.Join(homeDir, ".observer", "config.toml")
	} else if p, err := config.ResolveGlobalPath(""); err == nil {
		def = p
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		abs = configPath
	}
	if def != "" && filepath.Clean(abs) == filepath.Clean(def) {
		return ""
	}
	return configPath
}

// dashboardBinaryInstall is dashapi.Options.BinaryInstall (U12): the
// install channel of the running binary and whether hooks and MCP entries
// name the package manager's launcher. No path: the route is remote-readable.
func dashboardBinaryInstall() dashapi.BinaryInstall {
	out := dashapi.BinaryInstall{Channel: string(installshape.ChannelOther), Version: version, Registers: "binary"}
	binary, err := absoluteBinaryPath()
	if err != nil {
		return out
	}
	d := registrationDecision(binary)
	out.Channel = string(d.Channel)
	if d.Command != binary {
		out.Registers = "launcher"
	}
	return out
}

// binaryInstallLine is `observer components status`'s binary line (U12): the
// running binary's path, its install channel and what hooks and MCP entries
// invoke.
func binaryInstallLine() string {
	binary, err := absoluteBinaryPath()
	if err != nil {
		return "Binary: unknown (" + err.Error() + ")"
	}
	d := registrationDecision(binary)
	line := "Binary: " + binary + " (v" + version + ", " + installChannelLabel(d.Channel) + ")"
	if d.Command != binary {
		line += "; hooks and MCP entries run " + d.Command + ", which survives a package-manager upgrade"
	}
	return line
}

// installChannelLabel words an install channel for a status line.
func installChannelLabel(c installshape.Channel) string {
	switch c {
	case installshape.ChannelNPM:
		return "installed with npm"
	case installshape.ChannelPip:
		return "installed with pip"
	}
	return "an install.sh, archive or local build install"
}
