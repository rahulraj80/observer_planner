package proxyroute

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
)

// resolveCrushConfigPath returns the platform-native global Crush config
// (crush.json). Precedence: CRUSH_CONFIG env (a full file path) >
// $XDG_CONFIG_HOME/crush/crush.json > homeDir/.config/crush/crush.json.
// Crush (charmbracelet/crush) stores its global config under the XDG config
// dir on Linux/macOS; the proxy/route writer runs on the daemon OS. (The
// project-local .crush/crush.json is a per-repo override, out of scope for
// a global route write.)
func resolveCrushConfigPath(homeDir string) string {
	if env := strings.TrimSpace(os.Getenv("CRUSH_CONFIG")); env != "" {
		return env
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "crush", "crush.json")
	}
	return filepath.Join(homeDir, ".config", "crush", "crush.json")
}

// RegisterCrush points Crush's OpenAI-compatible provider at the observer
// proxy by ADDITIVELY setting `base_url` on the `providers.openai` entry in
// crush.json. crush.json embeds literal provider keys, so only the base_url
// member of the openai provider is added and nothing else is read into a
// result or an error.
//
// Guard semantics mirror RegisterKimiCode:
//   - config file absent      -> ConfigMissing (benign skip)
//   - no providers.openai     -> Error (nothing to route)
//   - base_url absent         -> add it (Added)
//   - base_url already loopback/observer -> AlreadySet (idempotent)
//   - base_url points elsewhere -> Error (refuse to overwrite a foreign URL)
//
// The write follows the probe-writer credential-file discipline
// (probe_write.go, T-657): one member spliced in through cfgfmt (every other
// byte and key order kept), no backup copy, mode kept, a symlinked layout
// refused, and no error quotes the file.
func (r *Registrar) RegisterCrush() RegistrationResult {
	return r.note(r.writeCrush(), openAICompatProxyURL(r.opts.ProxyPort))
}

// writeCrush is RegisterCrush without the provenance record.
func (r *Registrar) writeCrush() RegistrationResult {
	const what = "proxyroute.crush"
	path := resolveCrushConfigPath(r.opts.HomeDir)
	want := openAICompatProxyURL(r.opts.ProxyPort)
	res := RegistrationResult{Tool: "crush", ConfigPath: path, BaseURL: want, DryRun: r.opts.DryRun}
	res.AlreadySet, res.Added, res.Error = r.addProbeBaseURL(what, cfgfmt.FormatJSON, path, crushBaseURLPath, want, &res)
	return res
}
