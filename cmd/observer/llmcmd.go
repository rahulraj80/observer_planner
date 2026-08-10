package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/llmconn"
	"github.com/marmutapp/superbased-observer/internal/llmkeyring"
)

// newLLMCmd is the `observer llm` group: manage the OPTIONAL LLM analysis
// connections (daily-briefing Phase B, docs/design/standup-briefing.md §3).
// Connections are static config authored here (or in config.toml); secrets
// are env-var or keychain NAMES, never literals. A loopback connection is
// keyless; a remote one is inert until allow_remote=true AND a key ref exists.
func newLLMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "llm",
		Short: "Manage LLM analysis connections for the daily briefing",
		Long: "The daily briefing's OPTIONAL LLM surfaces (narrative paragraph +\n" +
			"grounded Q&A) run against connections you point them at. Loopback\n" +
			"servers (Ollama/vLLM/llama.cpp) are keyless and make ZERO network\n" +
			"calls. Remote providers need an explicit allow_remote=true plus an\n" +
			"api_key_env (an ENV VAR NAME — never the key). Nothing is configured\n" +
			"by default: with no connections the briefing is data-only.\n\n" +
			"  observer llm list\n" +
			"  observer llm setup\n" +
			"  observer llm status local-ollama\n" +
			"  observer llm add --id local-ollama --kind local_openai_compat --base-url http://127.0.0.1:11434/v1\n" +
			"  observer llm remove local-ollama",
	}
	cmd.AddCommand(newLLMListCmd())
	cmd.AddCommand(newLLMSetupCmd())
	cmd.AddCommand(newLLMStatusCmd())
	cmd.AddCommand(newLLMAddCmd())
	cmd.AddCommand(newLLMRemoveCmd())
	return cmd
}

// loadLLMConfig resolves the global config.toml path (shared --config flag)
// and loads it.
func loadLLMConfig(configPath string) (string, config.Config, error) {
	path, err := config.ResolveGlobalPath(configPath)
	if err != nil {
		return "", config.Config{}, err
	}
	cfg, err := config.Load(config.LoadOptions{GlobalPath: path})
	if err != nil {
		return "", config.Config{}, fmt.Errorf("load config: %w", err)
	}
	return path, cfg, nil
}

func newLLMListCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured LLM analysis connections",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, cfg, err := loadLLMConfig(configPath)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			conns := cfg.LLM.ToConnections()
			if len(conns) == 0 {
				fmt.Fprintln(out, "no LLM analysis connections configured (config: "+path+")")
				fmt.Fprintln(out, "hint: run `observer llm setup` to auto-discover a local Ollama/vLLM server, or `observer llm add`.")
				return nil
			}
			fmt.Fprintf(out, "%-20s %-18s %-8s %-14s %-12s %s\n", "ID", "KIND", "ENABLED", "MODEL", "ROLES", "BASE_URL")
			for _, c := range conns {
				roles := "all"
				if len(c.TaskRoles) > 0 {
					parts := make([]string, 0, len(c.TaskRoles))
					for _, r := range c.TaskRoles {
						parts = append(parts, string(r))
					}
					roles = strings.Join(parts, ",")
				}
				suffix := ""
				if c.Remote() && !c.AllowRemote {
					suffix = " (inert: allow_remote off)"
				}
				fmt.Fprintf(out, "%-20s %-18s %-8v %-14s %-12s %s%s\n", c.ID, c.Kind, c.Enabled, c.DefaultModel, roles, c.BaseURL, suffix)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config.toml (defaults to ~/.observer/config.toml)")
	return cmd
}

func newLLMStatusCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "status <id>",
		Short: "Live health check for one connection (reachability + model list)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			_, cfg, err := loadLLMConfig(configPath)
			if err != nil {
				return err
			}
			conn, ok := findLLMConnection(cfg, id)
			if !ok {
				return fmt.Errorf("no connection %q — see `observer llm list`", id)
			}
			out := cmd.OutOrStdout()
			if !conn.Enabled {
				fmt.Fprintln(out, "connection disabled — never selected")
			}
			if conn.Remote() && !conn.AllowRemote {
				fmt.Fprintln(out, "remote connection inert: allow_remote=false")
			}
			hv := os.Getenv(conn.APIKeyEnv)
			if hv == "" && conn.APIKeyKeychain != "" {
				// Resolve the OS-keychain ref (the dashboard's key-save
				// endpoint writes keys under this same store).
				v, err := llmkeyring.Open(filepath.Dir(cfg.Observer.DBPath)).Load(conn.APIKeyKeychain)
				if err == nil {
					hv = v
				} else if !errors.Is(err, llmkeyring.ErrNoKey) {
					fmt.Fprintf(out, "keychain credential %q: %v\n", conn.APIKeyKeychain, err)
				}
			}
			if conn.Remote() && hv == "" {
				fmt.Fprintf(out, "no credential for env %q / keychain %q — set it before this connection can be used\n", conn.APIKeyEnv, conn.APIKeyKeychain)
			}
			client := llmconn.NewClient(conn, hv)
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			h, models := client.Test(ctx)
			fmt.Fprintf(out, "%s: %s (%dms)\n", id, h.Message, h.LatencyMS)
			if len(models) > 0 {
				fmt.Fprintf(out, "models (%d): %s\n", len(models), strings.Join(models, ", "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config.toml (defaults to ~/.observer/config.toml)")
	return cmd
}

func newLLMAddCmd() *cobra.Command {
	var (
		configPath string
		connID     string
		conn       config.LLMConnectionConfig
		headers    []string
	)
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add (or update) one LLM analysis connection in config.toml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, cfg, err := loadLLMConfig(configPath)
			if err != nil {
				return err
			}
			connID = strings.TrimSpace(connID)
			if connID == "" {
				return fmt.Errorf("--id is required")
			}
			conn.Kind = strings.TrimSpace(conn.Kind)
			if conn.Kind == "" {
				conn.Kind = string(llmconn.KindOpenAICompat)
			}
			if len(headers) > 0 {
				if conn.Headers == nil {
					conn.Headers = map[string]string{}
				}
				for _, h := range headers {
					name, val, ok := strings.Cut(h, ":")
					name, val = strings.TrimSpace(name), strings.TrimSpace(val)
					if !ok || name == "" {
						return fmt.Errorf("--header %q must be \"Name: Value\"", h)
					}
					conn.Headers[name] = val
				}
			}
			if cfg.LLM.Connections == nil {
				cfg.LLM.Connections = map[string]config.LLMConnectionConfig{}
			}
			cfg.LLM.Connections[connID] = conn
			if err := config.Validate(cfg); err != nil {
				return fmt.Errorf("refusing to save: %w", err)
			}
			if err := config.WriteToml(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "saved %q (config: %s)\n", connID, path)
			if pokeReload() {
				fmt.Fprintln(cmd.OutOrStdout(), "daemon reloaded — the briefing picks this up immediately")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "no running daemon detected — applies on next start")
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&connID, "id", "", "Connection id (slug, e.g. local-ollama) — required")
	f.StringVar(&configPath, "config", "", "Path to config.toml (defaults to ~/.observer/config.toml)")
	f.StringVar(&conn.Name, "name", "", "Human label (defaults to the id)")
	f.StringVar(&conn.Kind, "kind", "", "Kind: anthropic | openai_compat (default) | local_openai_compat")
	f.StringVar(&conn.BaseURL, "base-url", "", "Endpoint base, e.g. http://127.0.0.1:11434/v1 — required")
	f.StringVar(&conn.APIKeyEnv, "api-key-env", "", "ENV VAR NAME holding the key (never the key) — required for remote")
	f.StringVar(&conn.APIKeyKeychain, "api-key-keychain", "", "OS-keychain label holding the key (alternative to api_key_env)")
	f.StringSliceVar(&conn.Models, "models", nil, "Available models (comma-separated)")
	f.StringVar(&conn.DefaultModel, "default-model", "", "Default model for analysis calls")
	f.StringSliceVar(&conn.TaskRoles, "task-roles", nil, "Roles allowed: narrative,ask,ask_past,coach,summarize,triage,diagnose (empty = all)")
	f.BoolVar(&conn.Enabled, "enabled", true, "Enable the connection")
	f.BoolVar(&conn.AllowRemote, "allow-remote", false, "Opt into a non-loopback host (required to enable a remote connection)")
	f.IntVar(&conn.TimeoutMS, "timeout-ms", 0, "Per-call timeout in ms (0 = client default)")
	f.IntVar(&conn.MaxTokens, "max-tokens", 0, "Reply cap in tokens (0 = client default)")
	f.IntVar(&conn.NumCtx, "num-ctx", 0, "Context-window hint, passed ONLY to loopback hosts")
	f.StringArrayVar(&headers, "header", nil, `Extra HTTP header "Name: Value" (repeatable)`)
	return cmd
}

func newLLMRemoveCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove one LLM analysis connection from config.toml",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			path, cfg, err := loadLLMConfig(configPath)
			if err != nil {
				return err
			}
			if _, ok := cfg.LLM.Connections[id]; !ok {
				return fmt.Errorf("no connection %q — nothing removed", id)
			}
			delete(cfg.LLM.Connections, id)
			if err := config.WriteToml(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %q (config: %s)\n", id, path)
			if pokeReload() {
				fmt.Fprintln(cmd.OutOrStdout(), "daemon reloaded")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "no running daemon detected — applies on next start")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config.toml (defaults to ~/.observer/config.toml)")
	return cmd
}

// newLLMSetupCmd auto-discovers reachable local OpenAI-compatible servers
// (Ollama 11434, vLLM/llama.cpp 8000) and adds a keyless loopback connection
// for each, discovering its model list. Never touches remote providers.
func newLLMSetupCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Auto-discover a local model server and add it as a connection",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, cfg, err := loadLLMConfig(configPath)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			servers := probeLocalModelServers(ctx)
			if len(servers) == 0 {
				fmt.Fprintln(out, "no reachable local model server found (tried 127.0.0.1:11434 and 127.0.0.1:8000).")
				fmt.Fprintln(out, "start Ollama (`ollama serve`), then re-run `observer llm setup`, or add one manually:")
				fmt.Fprintln(out, "  observer llm add --id local-ollama --kind local_openai_compat --base-url http://127.0.0.1:11434/v1")
				return nil
			}
			if cfg.LLM.Connections == nil {
				cfg.LLM.Connections = map[string]config.LLMConnectionConfig{}
			}
			for _, srv := range servers {
				id := slugFromURL(srv.BaseURL)
				if _, exists := cfg.LLM.Connections[id]; exists {
					fmt.Fprintf(out, "%s already configured — leaving as-is\n", id)
					continue
				}
				conn := config.LLMConnectionConfig{
					Name:    srv.Label,
					Kind:    string(llmconn.KindLocalOpenAI),
					BaseURL: srv.BaseURL,
					Enabled: true,
				}
				// Discover the model list + a default from the running server.
				if h, models := llmconn.NewClient(llmconn.Connection{
					Kind:    llmconn.KindLocalOpenAI,
					BaseURL: srv.BaseURL,
				}, "").Test(ctx); h.OK && len(models) > 0 {
					conn.Models = models
					conn.DefaultModel = models[0]
				}
				cfg.LLM.Connections[id] = conn
				fmt.Fprintf(out, "added %s (%s) — default model %q\n", id, srv.Label, conn.DefaultModel)
			}
			if err := config.Validate(cfg); err != nil {
				return fmt.Errorf("refusing to save: %w", err)
			}
			if err := config.WriteToml(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(out, "saved (config: %s)\n", path)
			if pokeReload() {
				fmt.Fprintln(out, "daemon reloaded — the briefing picks this up immediately")
			} else {
				fmt.Fprintln(out, "no running daemon detected — applies on next start")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config.toml (defaults to ~/.observer/config.toml)")
	return cmd
}

// findLLMConnection looks up a connection by id from the converted list.
func findLLMConnection(cfg config.Config, id string) (llmconn.Connection, bool) {
	for _, c := range cfg.LLM.ToConnections() {
		if c.ID == id {
			return c, true
		}
	}
	return llmconn.Connection{}, false
}

// slugFromURL derives a stable connection id from a base_url host
// (http://127.0.0.1:11434/v1 → local-127-0-0-1-11434).
func slugFromURL(baseURL string) string {
	id := strings.ToLower(strings.TrimRight(baseURL, "/"))
	id = strings.ReplaceAll(id, "http://", "")
	id = strings.ReplaceAll(id, "https://", "")
	id = strings.ReplaceAll(id, "/v1", "")
	id = strings.ReplaceAll(id, ":", "-")
	id = strings.ReplaceAll(id, "/", "-")
	id = strings.ReplaceAll(id, ".", "-")
	id = strings.Trim(id, "-")
	return "local-" + id
}
