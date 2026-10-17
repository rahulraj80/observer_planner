// clinecli.go — `observer cline-cli` launcher subcommand.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// clineCompatProvider is the cline provider id whose base URL IS configurable
// (the native "openai" provider hardcodes api.openai.com and ignores
// OPENAI_BASE_URL — live-confirmed 2026-06-27; see docs/proxy-routing-blockers.md).
const clineCompatProvider = "openai-compatible"

// newClineCLICmd implements `observer cline-cli` — sets OPENAI_BASE_URL to
// the observer proxy's OpenAI-compatible endpoint and execs the user's
// `cline` (npm `cline` 3.x CLI) binary so its model traffic flows through
// the proxy.
//
// PROBE STATUS (2026-06-26): the live install's providers.json has no
// base-URL key and no config chokepoint to write — but a launch-time
// OPENAI_BASE_URL env MAY redirect it (integration registry: cline-cli
// Routability=probe_required). This launcher is the convenience that lets
// the operator run that probe in one command. Until a live turn confirms an
// api_turns row, the adapter matrix keeps cline-cli's PROXY cell empty
// (SURFACE=probe) — the launcher existing does NOT claim verified routing.
func newClineCLICmd() *cobra.Command {
	var (
		configPath   string
		proxyURL     string
		binPath      string
		continueFrom string
		carry        string
		fromMessage  int
		fromTime     string
		attach       *bool
		noAttach     *bool
		resume       *string
	)
	cmd := &cobra.Command{
		Use:   "cline-cli [-- cline-args...]",
		Short: "Launch Cline CLI with traffic routed through the observer proxy (probe)",
		Long: "Wraps `cline` (the npm Cline CLI) with OPENAI_BASE_URL pointed at\n" +
			"the observer proxy's OpenAI-compatible endpoint (…/v1).\n\n" +
			"PROBE: cline-cli's honoring of OPENAI_BASE_URL is unconfirmed on a\n" +
			"live install. Run one turn through this launcher, then check that a\n" +
			"new api_turns row appeared (`observer doctor cline-cli` or the\n" +
			"dashboard). The adapter matrix keeps cline-cli's PROXY cell empty\n" +
			"until that verification.\n\n" +
			"With --continue-from <session-id> the launcher distills a handover\n" +
			"from that session and seeds it via cline's -i/--tui interactive\n" +
			"flag (delivery=inject_prompt). See docs/session-handoff.md.\n\n" +
			"No openai-compatible provider yet (e.g. right after a guided\n" +
			"install, or another provider chosen): cline starts UNROUTED with\n" +
			"its own onboarding / provider and the launcher says so. To route,\n" +
			"run once `cline auth openai-compatible -k <key> -m <model> -b\n" +
			"<proxy>/v1`; later launches then route automatically.\n\n" +
			"An openai-compatible provider pointed at another service (Azure,\n" +
			"OpenRouter, an internal gateway) is never retargeted: its key\n" +
			"belongs to that service, so cline runs unrouted with it. One on the\n" +
			"OpenAI endpoint is routed for the session and its endpoint is put\n" +
			"back when cline exits.\n\n" +
			"All arguments after the subcommand are forwarded to cline. Use `--`\n" +
			"to separate observer flags from cline flags. NEVER touches API keys\n" +
			"- your provider credentials must already be in the environment.\n\n" +
			"Requires a running observer proxy (`observer start`).",
		SilenceErrors:      true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			args, done, err := launcherArgsOrDone(cmd, args)
			if done {
				return err
			}
			// Attach-by-default (attach-all-launchers): hand the PTY to the
			// daemon when attach resolves. cline-cli routes via the
			// openai-compatible provider's persisted baseUrl (applied by the
			// daemon-spawned inner launcher), so forward NO proxy env (attachEnv
			// nil) and no --no-proxy-route flag exists (noProxyRoute nil). A
			// leading utility subcommand (auth/config/…) or the continue-from
			// family forces the bare path — cline's `-p` is `--plan`, NOT a
			// headless-prompt flag, so it is deliberately NOT an incompatible
			// predicate. toolArgs is the RAW operator remainder — the inner
			// launcher re-applies the `-P openai-compatible` provider prepend.
			outcome, err := launcherAttach(cmd.Context(), launcherAttachSpec{
				tool:          "cline-cli",
				configPath:    configPath,
				proxyOverride: proxyURL,
				proxyFlag:     "--proxy",
				flagAttach:    *attach,
				flagNoAttach:  *noAttach,
				incompatible: continueFamilyEngaged(continueFrom, carry, fromMessage, fromTime) ||
					argsLeadWithSubcommand(args, clineSubcommands),
				passthrough: append(clineAttachPassthrough(binPath), resumeAttachPassthrough(*resume)...),
				toolArgs:    args,
				stderr:      cmd.ErrOrStderr(),
			})
			if outcome.handled {
				return err
			}
			// Native resume: `--resume <id>` → `cline --id <id>` (LEADS the user
			// args; the `-P openai-compatible` provider select is prepended below).
			resumedArgs, releaseResume, okResume, resumeErr := applyLauncherResume(launcherResumeSpec{
				verb: "cline-cli", label: "cline-cli", configPath: configPath, id: *resume,
				continueFrom: continueFrom, carry: carry, fromMessage: fromMessage, fromTime: fromTime,
				args: args, stderr: cmd.ErrOrStderr(),
			})
			if !okResume {
				return resumeErr
			}
			defer releaseResume()
			args = resumedArgs
			cfg, err := config.Load(config.LoadOptions{GlobalPath: configPath})
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			resolved := resolveProxyURL(cfg.Proxy.Port, proxyURL)
			bin, err := resolveToolBin("cline-cli", binPath, "--cline-path", configPath, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			// Point cline's openai-compatible provider at the proxy. A leading
			// utility subcommand (auth/config/doctor/…) makes no model call, so
			// it must not depend on — or be blocked by — the provider file:
			// `cline auth` and `cline doctor` are exactly how a missing or broken
			// provider gets fixed.
			selectProvider := true
			if !argsLeadWithSubcommand(args, clineSubcommands) {
				route, rerr := prepareClineRoute(strings.TrimRight(resolved, "/")+"/v1", cfg.Proxy.OpenAIUpstream, time.Now())
				if rerr != nil {
					return rerr
				}
				if route.note != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), route.note)
				}
				if route.restore != nil {
					// Sol A5-03: put the operator's endpoint back after cline
					// exits (compare-and-swap; a change they made wins).
					defer func() {
						// The error names an absolute path; launcher text must
						// not carry host detail, so only the fixed spelling is
						// printed.
						if err := route.restore(); err != nil {
							fmt.Fprintf(cmd.ErrOrStderr(),
								"observer cline-cli: could not restore your %s endpoint in %s; check it before your next plain `cline` run\n",
								clineCompatProvider, clineProvidersDisplay)
						}
					}()
				}
				selectProvider = route.action == clineRouteSetBaseURL
			}
			// Seed the handover onto the RAW user args FIRST, before the
			// `-P openai-compatible` provider selection is prepended. If we
			// injected after the prepend, the bare value `openai-compatible`
			// would trip forwardedPromptConflict and raise a false collision.
			var continueDir string
			if continueFrom != "" {
				seeded, cwd, cerr := continueFromArgs(cmd.Context(), continueFromParams{
					tool:        "cline-cli",
					label:       "cline",
					configPath:  configPath,
					sessionID:   continueFrom,
					carry:       carry,
					fromMessage: fromMessage,
					fromTime:    fromTime,
					args:        args,
					// cline `-i`/`--tui` is a BOOLEAN flag that opens the
					// interactive TUI seeded with the (separate positional)
					// prompt, auto-submitted. `injectFlagValue` emits `-i`
					// then the handover positional. cline's `-p` is `--plan`
					// (NOT a headless-prompt flag) so it is NOT a conflict.
					inject: promptInjection{
						Kind:          injectFlagValue,
						Flag:          "-i",
						ConflictFlags: []string{"-i", "--tui"},
						Subcommands:   clineSubcommands,
					},
					stderr: cmd.ErrOrStderr(),
				})
				if cerr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "observer cline: continue-from failed: %v\n", cerr)
					return cerr
				}
				args = seeded
				continueDir = cwd
			}
			// cline routes via the openai-compatible provider's persisted
			// baseUrl, NOT an env var — so select that provider and forward the
			// (possibly seeded) user's args. No env injection. An unrouted
			// launch (fresh install, or another provider configured) keeps
			// cline's own provider selection / first-run onboarding and
			// suppresses the "routing via" note, which would be false.
			childArgs := args
			var routeFile func() (string, bool)
			if selectProvider {
				childArgs = append([]string{"-P", clineCompatProvider}, args...)
				routeFile = readClineCompatBaseURL
			}
			return runEnvLauncher(envLauncherSpec{
				tool:       "cline-cli",
				bin:        bin,
				args:       childArgs,
				configPath: configPath,
				dir:        continueDir,
				proxyURL:   resolved,
				env:        nil,
				dbPath:     cfg.Observer.DBPath,
				unrouted:   !selectProvider,
				// T-208: the managed launch budget check reads the selected
				// provider's persisted baseUrl back as launch evidence, and
				// checks only the operator's own args for selectors.
				routeFile: routeFile,
				routeArgs: args,
				stderr:    cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config.toml (defaults to ~/.observer/config.toml)")
	cmd.Flags().StringVar(&proxyURL, "proxy", "", "Override the observer proxy URL (default: http://127.0.0.1:<cfg.proxy.port>)")
	cmd.Flags().StringVar(&binPath, "cline-path", "", "Path to the cline binary (default: resolve `cline` on PATH)")
	cmd.Flags().StringVar(&continueFrom, "continue-from", "", "Session id to continue from: distill a handover from that session and seed it via cline's -i/--tui interactive flag (delivery=inject_prompt). See docs/session-handoff.md.")
	cmd.Flags().StringVar(&carry, "carry", "", "Carry mode for --continue-from: metadata|distilled|distilled_tail|full|full_cache (default from [handoff] config)")
	cmd.Flags().IntVar(&fromMessage, "from-message", 0, "With --continue-from: fork after this 1-based transcript message (default: last message)")
	cmd.Flags().StringVar(&fromTime, "from-time", "", "With --continue-from: fork after the last message at or before this RFC3339 time")
	attach, noAttach = registerAttachFlags(cmd, "cline-cli")
	resume = registerResumeFlag(cmd, "cline-cli")
	return cmd
}

// clineAttachPassthrough forwards --cline-path to the daemon-spawned inner
// `observer cline-cli` launcher when the operator overrode the binary path;
// nil otherwise.
func clineAttachPassthrough(binPath string) []string {
	if binPath != "" {
		return []string{"--cline-path", binPath}
	}
	return nil
}

// clineSubcommands are the cline argv tokens that are subcommands, not a
// forwarded positional prompt — so forwardedPromptConflict does not misread
// e.g. `cline auth` as a two-prompt collision with the seeded handover.
var clineSubcommands = map[string]bool{
	"auth": true, "config": true, "plugin": true, "skill": true,
	"connect": true, "mcp": true, "doctor": true, "history": true,
	"hook": true, "schedule": true, "hub": true, "dashboard": true,
	"update": true, "version": true, "kanban": true,
}

// clineProvidersDisplay is how the launcher names cline's provider file in
// anything it prints: a fixed, home-relative spelling, never the resolved
// absolute path (launcher terminal text must not carry host detail).
const clineProvidersDisplay = "~/.cline/data/settings/providers.json"

// clineRouteAction is what the launcher does for one state of cline's
// provider file.
type clineRouteAction int

const (
	// clineRouteSetBaseURL: the openai-compatible entry exists and its
	// endpoint is one the proxy reaches (already the proxy, or the proxy's
	// own OpenAI upstream): point its baseUrl at the proxy (preserving every
	// other field), select it, and put the old baseUrl back after the session.
	clineRouteSetBaseURL clineRouteAction = iota
	// clineRouteOnboard: no provider is configured at all (a fresh install,
	// the dashboard's guided-install case) — touch nothing and launch cline
	// UNROUTED so its own first-run onboarding / sign-in runs, with a note on
	// how to opt into proxy routing. A keyless openai-compatible entry forced
	// with -P would instead start cline into a provider whose first prompt
	// fails upstream.
	clineRouteOnboard
	// clineRouteDirect: the operator configured a DIFFERENT provider and no
	// openai-compatible entry — launch cline unrouted with their own provider
	// rather than hijack it with a keyless openai-compatible one. Sessions are
	// still captured from ~/.cline by the watcher.
	clineRouteDirect
	// clineRouteForeignCompat: the openai-compatible entry points at an
	// endpoint the proxy cannot route to (Azure, OpenRouter, an internal
	// gateway, another local proxy). Its stored credential belongs to THAT
	// host; retargeting the entry at the proxy would forward the credential to
	// the proxy's fixed OpenAI upstream instead, and leave the user's
	// provider pointed at Observer after cline exits (Sol A5-03, D-119). The
	// entry is left untouched and cline runs unrouted with it.
	clineRouteForeignCompat
)

// clineCompatTarget classifies where an existing openai-compatible entry's
// baseUrl sends the entry's credential.
type clineCompatTarget int

// The zero value means no openai-compatible entry was classified.
const (
	// clineTargetObserver: the entry already points at this node's proxy
	// (the operator ran the `cline auth ... -b <proxy>/v1` the notes print).
	clineTargetObserver clineCompatTarget = iota + 1
	// clineTargetOpenAI: no baseUrl (the OpenAI default) or the proxy's own
	// OpenAI upstream host. Routing through the proxy reaches the SAME host
	// with the same credential, so it is safe to route.
	clineTargetOpenAI
	// clineTargetForeign: any other endpoint. Not safe to route.
	clineTargetForeign
)

// clineProvidersState is the routing-relevant shape of providers.json. Only
// the provider ids and the openai-compatible entry's baseUrl are inspected;
// no credential value (keys, tokens) is read.
type clineProvidersState struct {
	missing      bool              // no providers.json yet
	hasCompat    bool              // an openai-compatible entry exists
	compatTarget clineCompatTarget // where that entry's baseUrl points
	configured   int               // number of provider entries
}

// clineRouteRules is the ordered decision table, walked top-down; the first
// matching row wins. One test case per row (clinecli_test.go).
var clineRouteRules = []struct {
	name   string
	match  func(clineProvidersState) bool
	action clineRouteAction
}{
	{"fresh install: no providers.json", func(s clineProvidersState) bool { return s.missing }, clineRouteOnboard},
	{"openai-compatible already pointed at the proxy", func(s clineProvidersState) bool {
		return s.hasCompat && s.compatTarget == clineTargetObserver
	}, clineRouteSetBaseURL},
	{"openai-compatible on the OpenAI endpoint the proxy forwards to", func(s clineProvidersState) bool {
		return s.hasCompat && s.compatTarget == clineTargetOpenAI
	}, clineRouteSetBaseURL},
	{"openai-compatible on an endpoint the proxy cannot route", func(s clineProvidersState) bool { return s.hasCompat }, clineRouteForeignCompat},
	{"no provider configured yet", func(s clineProvidersState) bool { return s.configured == 0 }, clineRouteOnboard},
	{"another provider configured", func(clineProvidersState) bool { return true }, clineRouteDirect},
}

// classifyClineCompatTarget decides where an openai-compatible baseUrl sends
// its credential. proxyBase is this node's proxy endpoint (…/v1);
// openAIUpstream is the proxy's fixed OpenAI upstream ([proxy].openai_upstream),
// the only host a request to proxyBase/v1 is forwarded to.
func classifyClineCompatTarget(raw any, proxyBase, openAIUpstream string) clineCompatTarget {
	s, isString := raw.(string)
	if raw == nil || (isString && strings.TrimSpace(s) == "") {
		return clineTargetOpenAI
	}
	if !isString {
		return clineTargetForeign
	}
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Host == "" {
		return clineTargetForeign
	}
	if p, perr := url.Parse(proxyBase); perr == nil && p.Host != "" &&
		strings.EqualFold(u.Scheme, p.Scheme) && strings.EqualFold(u.Host, p.Host) {
		return clineTargetObserver
	}
	if o, oerr := url.Parse(openAIUpstream); oerr == nil && o.Host != "" &&
		strings.EqualFold(u.Scheme, o.Scheme) && strings.EqualFold(u.Host, o.Host) {
		return clineTargetOpenAI
	}
	return clineTargetForeign
}

// decideClineRoute walks clineRouteRules for s.
func decideClineRoute(s clineProvidersState) clineRouteAction {
	for _, r := range clineRouteRules {
		if r.match(s) {
			return r.action
		}
	}
	return clineRouteDirect // unreachable: the last row matches everything
}

// clineRoute is prepareClineRoute's plain result: the action taken and the
// one-line note (possibly empty) to show the operator before cline starts.
type clineRoute struct {
	action clineRouteAction
	note   string
	// restore, when non-nil, puts back the baseUrl this launch changed. The
	// launcher calls it after cline exits. Nil when nothing was changed.
	restore func() error
}

// prepareClineRoute applies clineRouteRules to
// ~/.cline/data/settings/providers.json: only an EXISTING openai-compatible
// entry whose endpoint the proxy can safely reach is touched (its baseUrl
// pointed at baseURL, and put back after the session); it never creates an
// entry and NEVER writes an api key. openAIUpstream is the proxy's fixed
// OpenAI upstream. Every error it returns carries a bounded terminal message
// (the launcher silences Cobra), so a dashboard launch that cannot start says
// why instead of exiting 1 with no output.
func prepareClineRoute(baseURL, openAIUpstream string, now time.Time) (clineRoute, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return clineRoute{}, newVisibleLauncherError(err,
			"observer cline-cli: cannot locate your home directory to configure cline's provider")
	}
	return prepareClineRouteAt(filepath.Join(home, ".cline", "data", "settings", "providers.json"), baseURL, openAIUpstream, now)
}

// readClineCompatBaseURL re-reads the openai-compatible provider's persisted
// settings.baseUrl from ~/.cline/data/settings/providers.json (T-208 launch
// evidence). Only that one field is read; no credential is.
func readClineCompatBaseURL() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	return readClineCompatBaseURLAt(filepath.Join(home, ".cline", "data", "settings", "providers.json"))
}

// readClineCompatBaseURLAt is readClineCompatBaseURL over an explicit path.
func readClineCompatBaseURLAt(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var root struct {
		Providers map[string]struct {
			Settings struct {
				BaseURL string `json:"baseUrl"`
			} `json:"settings"`
		} `json:"providers"`
	}
	if json.Unmarshal(data, &root) != nil {
		return "", false
	}
	entry, ok := root.Providers[clineCompatProvider]
	if !ok || entry.Settings.BaseURL == "" {
		return "", false
	}
	return entry.Settings.BaseURL, true
}

// prepareClineRouteAt is prepareClineRoute over an explicit file path.
func prepareClineRouteAt(path, baseURL, openAIUpstream string, now time.Time) (clineRoute, error) {
	authHint := fmt.Sprintf("cline auth %s -k <key> -m <model> -b %s", clineCompatProvider, baseURL)
	root := map[string]any{}
	state := clineProvidersState{}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		state.missing = true
	case err != nil:
		return clineRoute{}, newVisibleLauncherError(fmt.Errorf("read %s: %w", path, err), fmt.Sprintf(
			"observer cline-cli: cannot read %s; check its permissions, then relaunch", clineProvidersDisplay))
	default:
		if uerr := json.Unmarshal(data, &root); uerr != nil || root == nil {
			if uerr == nil {
				uerr = errors.New("top level is null")
			}
			return clineRoute{}, newVisibleLauncherError(fmt.Errorf("parse %s: %w", path, uerr), fmt.Sprintf(
				"observer cline-cli: %s is not valid JSON; fix it or move it aside (cline recreates it), then relaunch",
				clineProvidersDisplay))
		}
	}
	providers, _ := root["providers"].(map[string]any)
	state.configured = len(providers)
	if compat, ok := providers[clineCompatProvider].(map[string]any); ok {
		state.hasCompat = true
		compatSettings, _ := compat["settings"].(map[string]any)
		state.compatTarget = classifyClineCompatTarget(compatSettings["baseUrl"], baseURL, openAIUpstream)
	}

	route := clineRoute{action: decideClineRoute(state)}
	switch route.action {
	case clineRouteForeignCompat:
		route.note = fmt.Sprintf("observer cline-cli: NOT routing through the observer proxy: cline's %q provider "+
			"points at its own endpoint, and the proxy cannot forward to that host, so routing it would send that "+
			"provider's key to the wrong service. Your provider is used directly and left unchanged (sessions are "+
			"still captured). To route, point an OpenAI provider at the proxy once: %s",
			clineCompatProvider, authHint)
		return route, nil
	case clineRouteOnboard:
		route.note = fmt.Sprintf("observer cline-cli: cline has no provider configured yet, so it starts with its own "+
			"sign-in/onboarding and is NOT routed through the observer proxy (sessions are still captured). To route "+
			"through Observer's proxy (accurate tokens, budgets, prompt guard), configure an OpenAI-compatible provider "+
			"pointed at it once: %s; after that `observer cline-cli` routes automatically", authHint)
		return route, nil
	case clineRouteDirect:
		route.note = fmt.Sprintf("observer cline-cli: NOT routing through the observer proxy: cline has a provider "+
			"configured but not %q, so your own provider is used directly (sessions are still captured). "+
			"To route through Observer, run once: %s", clineCompatProvider, authHint)
		return route, nil
	}
	entry := providers[clineCompatProvider].(map[string]any)
	settings, _ := entry["settings"].(map[string]any)
	if settings == nil {
		settings = map[string]any{}
		entry["settings"] = settings
	}
	// cline (3.0.68, grounded 2026-10-02) DISCARDS an entry's settings —
	// baseUrl included — and rewrites the entry from defaults when it lacks
	// its own updatedAt/tokenSource stamp. Fill only what is absent so the
	// baseUrl survives cline's first read.
	stamped := false
	if _, ok := entry["updatedAt"]; !ok {
		entry["updatedAt"] = now.UTC().Format("2006-01-02T15:04:05.000Z")
		stamped = true
	}
	if _, ok := entry["tokenSource"]; !ok {
		entry["tokenSource"] = "manual"
		stamped = true
	}
	if settings["baseUrl"] == baseURL && !stamped {
		return route, nil // already pointed at the proxy
	}
	original, hadBaseURL := settings["baseUrl"]
	changedBaseURL := original != baseURL
	settings["baseUrl"] = baseURL
	if err := writeClineProviders(path, root); err != nil {
		return clineRoute{}, newVisibleLauncherError(err, fmt.Sprintf(
			"observer cline-cli: cannot write %s; check the directory's permissions, then relaunch",
			clineProvidersDisplay))
	}
	if changedBaseURL {
		// Put the operator's own endpoint back when the session ends, so a
		// later plain `cline` run is not silently pointed at Observer. The
		// stamps stay: without them cline discards the entry's settings.
		route.restore = func() error { return restoreClineBaseURL(path, baseURL, original, hadBaseURL) }
	}
	return route, nil
}

// restoreClineBaseURL puts the openai-compatible entry's baseUrl back to what
// it was before this launch, as a compare-and-swap: only when the entry still
// carries the proxy URL this launch wrote. If the operator (or cline) changed
// it during the session, their value wins and nothing is written. Every other
// field is preserved, and no credential is read or written.
func restoreClineBaseURL(path, wrote string, original any, hadOriginal bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // removed during the session: nothing to put back
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	root := map[string]any{}
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		return fmt.Errorf("parse %s: not valid JSON, left as is", path)
	}
	providers, _ := root["providers"].(map[string]any)
	entry, _ := providers[clineCompatProvider].(map[string]any)
	settings, _ := entry["settings"].(map[string]any)
	if settings == nil || settings["baseUrl"] != wrote {
		return nil // changed during the session: theirs wins
	}
	if hadOriginal {
		settings["baseUrl"] = original
	} else {
		delete(settings, "baseUrl")
	}
	return writeClineProviders(path, root)
}

// writeClineProviders atomically writes root to path (0600; parent dirs 0700).
func writeClineProviders(path string, root map[string]any) error {
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return nil
}
