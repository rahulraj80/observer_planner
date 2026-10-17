// crush.go — `observer crush` launcher subcommand.

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/proxyroute"
	"github.com/marmutapp/superbased-observer/internal/routeprov"
)

// newCrushCmd implements `observer crush` — applies the SAME additive
// providers.openai.base_url write to crush.json that `observer init` uses
// (proxyroute.RegisterCrush, idempotent, never a key) and then execs the
// user's `crush` binary, so its model traffic flows through the observer
// proxy for accurate token capture.
//
// Crush routes via a CONFIG FILE, not an env var (registry RouteProviderJSON,
// live-verified 2026-07-09 via api_turns 23081/23080) — so unlike the env
// launchers there is nothing to inject at exec time; this launcher's value is
// ensuring the route exists before every launch, not only at init.
//
// Fail-open: a missing crush.json (ConfigMissing) or a foreign base_url the
// writer refuses to overwrite (Error) prints a one-line notice and launches
// anyway with the caller's own environment — identical posture to running
// bare `crush`. It never touches an API key.
func newCrushCmd() *cobra.Command {
	var (
		configPath string
		crushPath  string
		fork       docAssistedFlags
		attach     *bool
		noAttach   *bool
	)
	cmd := &cobra.Command{
		Use:   "crush [-- crush-args...]",
		Short: "Launch Crush with its OpenAI provider routed through the observer proxy",
		Long: "Applies the additive providers.openai.base_url route to crush.json\n" +
			"(the same write `observer init` performs - never reads or moves an API\n" +
			"key) and then execs `crush` with your own environment. Use `--` to\n" +
			"separate observer flags from crush flags.\n\n" +
			"If crush.json is absent or already points at a foreign base URL, the\n" +
			"launcher says so and launches unproxied - token capture then falls back\n" +
			"to observer's local crush adapter.\n\n" +
			"With --continue-from <session-id> the launch is DOC-ASSISTED: crush\n" +
			"has no initial-prompt seed (upstream issue #1791), so the launcher\n" +
			"writes the handover doc, prints its path for you to paste, and opens\n" +
			"the TUI in the source session's project root.",
		SilenceErrors:      true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			args, done, err := launcherArgsOrDone(cmd, args)
			if done {
				return err
			}
			// Attach gate (2026-10-03): default-on attach hands the PTY to
			// the daemon, whose inner `observer crush` re-applies the
			// crush.json route itself, so no proxy env crosses the socket.
			// `crush run` (non-interactive one-shot, internal/cmd/run.go),
			// help/version, or the handoff-fork family take the bare path.
			outcome, aErr := launcherAttach(cmd.Context(), launcherAttachSpec{
				tool:         "crush",
				configPath:   configPath,
				flagAttach:   *attach,
				flagNoAttach: *noAttach,
				incompatible: fork.engaged() ||
					argsLeadWithSubcommand(args, crushHeadlessSubcommands) ||
					argsContainHeadlessFlag(args, "-h", "--help", "-v", "--version"),
				passthrough: binPathPassthrough("--crush-path", crushPath),
				toolArgs:    args,
				stderr:      cmd.ErrOrStderr(),
			})
			if outcome.handled {
				return aErr
			}
			cfg, cErr := config.Load(config.LoadOptions{GlobalPath: configPath})
			if cErr != nil {
				return fmt.Errorf("load config: %w", cErr)
			}
			if reg, rErr := proxyroute.NewRegistrar(proxyroute.RegisterOptions{
				Writer:    routeprov.WriterLauncher,
				ProxyPort: cfg.Proxy.Port,
			}); rErr == nil {
				res := reg.RegisterCrush()
				switch {
				case res.ConfigMissing:
					fmt.Fprintln(cmd.ErrOrStderr(), crushNoConfigNotice(res.ConfigPath))
				case res.Error != nil:
					fmt.Fprintf(cmd.ErrOrStderr(),
						"observer crush: route write refused (%v) - launching unproxied\n", res.Error)
				case res.Added:
					fmt.Fprintf(cmd.ErrOrStderr(),
						"observer crush: routed providers.openai.base_url → %s (in %s)\n", res.BaseURL, res.ConfigPath)
				case res.AlreadySet:
					fmt.Fprintln(cmd.ErrOrStderr(), "observer crush: crush.json already routed through the observer proxy")
				}
			}
			bin, err := resolveToolBin("crush", crushPath, "--crush-path", configPath, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			dir, err := docAssistedContinue(cmd.Context(), cmd.ErrOrStderr(), configPath, "crush", "crush", fork)
			if err != nil {
				return err
			}
			return runSeedOnlyLaunchSeeded(configPath, cfg.Observer.DBPath, "crush", "crush", bin, args, dir)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config.toml (defaults to ~/.observer/config.toml)")
	cmd.Flags().StringVar(&crushPath, "crush-path", "", "Path to the crush binary (default: resolve `crush` on PATH)")
	registerDocAssistedFlags(cmd, "crush", &fork)
	attach, noAttach = registerAttachFlags(cmd, "crush")
	return cmd
}

// crushHeadlessSubcommands are the crush verbs that do not open the
// interactive TUI: `crush run` is "Run a single non-interactive prompt"
// (charmbracelet/crush internal/cmd/run.go, read 2026-10-03).
var crushHeadlessSubcommands = map[string]bool{"run": true}

// crushNoConfigNotice is the launcher's line when crush has no crush.json
// (T-685). It used to point at an `observer init` flag that does nothing for
// this case; it now names the file and the one step only the developer can
// take (a provider with their key), after which this launcher or `observer
// init` adds the route without reading the key.
func crushNoConfigNotice(path string) string {
	where := "crush.json"
	if path != "" {
		where = path
	}
	return "observer crush: no " + where + " yet - launching unproxied. To route crush through Observer: " +
		"give crush an OpenAI provider with your key (a providers.openai entry in " + where + "), " +
		"then run `observer crush` again (or `observer init`); Observer adds the proxy base_url itself and never reads or writes the key."
}
