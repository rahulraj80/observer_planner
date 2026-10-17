// aider.go — `observer aider` launcher subcommand.

package main

import (
	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// newAiderCmd implements `observer aider` — launches Aider (binary `aider`),
// routed through the observer proxy by injecting OPENAI_API_BASE (unless the
// operator already set it). Aider's own capture path is its per-repo
// .aider.chat.history.md transcript, watched independently of the proxy — the
// proxy route exists purely to land accurate api_turns rows and enable
// conversation compression, same as opencode/cline-cli/pi.
//
// Since 2026-10-03 (terminal-picker coverage) it also carries the shared
// attach gate and a DOC-ASSISTED --continue-from: aider's REPL has no
// initial-prompt seed (`--message` runs one turn and EXITS), so the handover
// doc is written to disk and the REPL opens in the source project. No native
// resume: aider has no session-id surface.
//
// It never touches API keys — only the base URL.
func newAiderCmd() *cobra.Command {
	var (
		configPath string
		binPath    string
		fork       docAssistedFlags
		attach     *bool
		noAttach   *bool
	)
	cmd := &cobra.Command{
		Use:   "aider [-- aider-args...]",
		Short: "Launch Aider, routed through the observer proxy",
		Long: "Wraps Aider (`aider`), routing its traffic through the observer\n" +
			"proxy by injecting OPENAI_API_BASE at the proxy's OpenAI-compatible\n" +
			"root (unless you've already set it yourself - your value always\n" +
			"wins). Aider's own .aider.chat.history.md transcript is observer's\n" +
			"local capture path regardless of the proxy route.\n\n" +
			"With --continue-from <session-id> the launch is DOC-ASSISTED: aider\n" +
			"has no initial-prompt seed (--message exits after one turn), so the\n" +
			"launcher writes the handover doc, prints its path for you to paste,\n" +
			"and opens the REPL in the source session's project root.\n\n" +
			"All arguments after the subcommand are forwarded to aider. Use `--`\n" +
			"to separate observer flags from aider flags. NEVER touches API keys.",
		SilenceErrors:      true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			args, done, err := launcherArgsOrDone(cmd, args)
			if done {
				return err
			}
			// Attach gate: default-on attach hands the PTY to the daemon,
			// whose inner `observer aider` re-injects OPENAI_API_BASE itself,
			// so no proxy env crosses the socket (aider has no --proxy flag).
			// A --message/--message-file one-shot, help/version, or the
			// handoff-fork family takes the bare path.
			outcome, aErr := launcherAttach(cmd.Context(), launcherAttachSpec{
				tool:         "aider",
				configPath:   configPath,
				flagAttach:   *attach,
				flagNoAttach: *noAttach,
				incompatible: fork.engaged() || aiderHeadless(args),
				passthrough:  binPathPassthrough("--aider-path", binPath),
				toolArgs:     args,
				stderr:       cmd.ErrOrStderr(),
			})
			if outcome.handled {
				return aErr
			}

			bin, err := resolveToolBin("aider", binPath, "--aider-path", configPath, cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			cfg, cErr := config.Load(config.LoadOptions{GlobalPath: configPath})
			if cErr != nil {
				return cErr
			}
			dir, err := docAssistedContinue(cmd.Context(), cmd.ErrOrStderr(), configPath, "aider", "aider", fork)
			if err != nil {
				return err
			}
			resolved := resolveProxyURL(cfg.Proxy.Port, "")
			return runEnvLauncher(envLauncherSpec{
				tool:       "aider",
				bin:        bin,
				args:       args,
				configPath: configPath,
				dir:        dir,
				proxyURL:   resolved,
				env:        map[string]string{"OPENAI_API_BASE": resolved + "/v1"},
				dbPath:     cfg.Observer.DBPath,
				stderr:     cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config.toml (defaults to ~/.observer/config.toml)")
	cmd.Flags().StringVar(&binPath, "aider-path", "", "Path to the aider binary (default: resolve `aider` on PATH)")
	registerDocAssistedFlags(cmd, "aider", &fork)
	attach, noAttach = registerAttachFlags(cmd, "aider")
	return cmd
}

// aiderHeadless reports whether a forwarded argv runs aider non-interactively:
// `--message`/`--msg`/`-m` and `--message-file`/`-f` "process reply then exit
// (disables chat mode)" (aider.chat/docs/config/options.html, 2026-10-03), and
// help/version print and exit.
func aiderHeadless(args []string) bool {
	return argsContainHeadlessFlag(args,
		"--message", "--msg", "-m", "--message-file", "-f",
		"-h", "--help", "--version")
}
