package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/telemetry"
	"github.com/marmutapp/superbased-observer/internal/telemetry/contract"
)

// telemetryDisclosure is what `observer telemetry consent <tier>` prints
// before applying. The wording is part of the consent surface: it states
// exactly what each tier ships and what never leaves.
const telemetryDisclosure = `TELEMETRY — what each tier sends (the sharing ladder's telemetry rungs):

  off         nothing collected, staged, or sent.
  baseline    content-free counters: daily activity, sessions and their
              durations, tool-use counts, token totals by model tier, error
              classes, app version and OS/arch, and for each prompt cycle the
              model and provider family, input and output sizes, cache
              percentage, end-to-end time, and whether the turn was your
              prompt, a system nudge or a tool call (classified on this
              machine; only the label is sent). Every field comes from a closed
              list in the published contract (1.2.0), which refuses any other
              field: no prompt or output text, paths, project names or commands.
  enrichment  reserved for opt-in enrichment; today it sends what baseline sends.
  deep        baseline + truncated, scrubbed command lines (its own disclosure,
              shown above when you choose it; 90-day server-side cap; purged
              when you step down).

WHERE:  SuperBased cloud (the telemetry lane at the [telemetry] endpoint,
        cloud.superbased.app by default), or your self-hosted reporter if you
        ran 'observer telemetry register'. Batches every ~15 min, size-capped.
WHO:    an install id derived from this machine's cloud device key (generated
        here and kept in the OS keychain; no account, nothing hardware-derived).
        'observer telemetry data' shows what the server holds;
        'observer telemetry delete --yes' removes it;
        'observer telemetry reset-id --yes' revokes it and starts a new key
        (this also signs the machine out of cloud intelligence).
KILL:   the OBSERVER_TELEMETRY=off env var stops collection and sending at any
        time.
`

// newTelemetryCmd is the `observer telemetry` command group (plan §D2):
// show / consent / enable / disable / register / reset-id.
func newTelemetryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Show and control usage telemetry (content-free baseline, on by default)",
		Long: "Inspect and control the usage telemetry collector. The content-free\n" +
			"baseline tier is on by default; `consent off` or OBSERVER_TELEMETRY=off stops\n" +
			"it. `show` reports the current posture; `consent` sets the tier after full\n" +
			"disclosure; `enable`/`disable` gate the daemon batcher; `data` and `delete`\n" +
			"show or remove what the server holds; `register` points the node at a\n" +
			"self-hosted reporter; `reset-id` rotates the install id.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newTelemetryShowCmd())
	cmd.AddCommand(newTelemetryConsentCmd())
	cmd.AddCommand(newTelemetryEnableCmd())
	cmd.AddCommand(newTelemetryDisableCmd())
	cmd.AddCommand(newTelemetryRegisterCmd())
	cmd.AddCommand(newTelemetryResetIDCmd())
	cmd.AddCommand(newTelemetryDataCmd())
	cmd.AddCommand(newTelemetryDeleteCmd())
	cmd.AddCommand(newTelemetryNudgeScanCmd())
	return cmd
}

func newTelemetryShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the current telemetry posture",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(config.LoadOptions{GlobalPath: ""})
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			tc := cfg.Telemetry
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "consent tier:  %s\n", tc.EffectiveTier())
			fmt.Fprintf(out, "collector:     %s\n", enabledWord(tc.Enabled))
			fmt.Fprintf(out, "endpoint:      %s\n", tc.Endpoint)
			if tc.Enabled {
				fmt.Fprintf(out, "batch:         every %ds ±10%%, cap %d bytes compressed\n",
					tc.BatchIntervalSeconds, tc.MaxBatchBytes)
			}
			if killed := config.TelemetryKillSwitchActive(); killed {
				fmt.Fprintln(out, "kill switch:   ENGAGED (OBSERVER_TELEMETRY is set) — nothing is emitted or sent")
			}
			if tc.Tier == "" {
				fmt.Fprintln(out, "\nTelemetry is off (default). `observer telemetry consent usage` opts in.")
			}
			return nil
		},
	}
}

func enabledWord(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

func newTelemetryConsentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "consent <tier>",
		Short: "Set the consent tier (off|baseline|enrichment|deep) after full disclosure",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tier := strings.ToLower(strings.TrimSpace(args[0]))
			if !contract.ValidTier(tier) {
				return fmt.Errorf("unknown tier %q — valid: off, baseline, enrichment, deep (legacy: usage, error)", tier)
			}
			resolved, err := config.ResolveGlobalPath("")
			if err != nil {
				return fmt.Errorf("resolve config path: %w", err)
			}
			cfg, err := config.Load(config.LoadOptions{GlobalPath: resolved})
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			// Deep-tier disclosure is explicit at consent time (plan §D2):
			// the CANONICAL contract text is shown verbatim and the consent
			// request is bound to its sha256 — the server records the
			// digest-bound receipt and refuses any other digest (BR-10.7).
			if tier == contract.TierDeep {
				fmt.Fprint(cmd.OutOrStdout(), contract.DeepDisclosure)
				fmt.Fprintln(cmd.OutOrStdout(), "---")
			}
			fmt.Fprint(cmd.OutOrStdout(), telemetryDisclosure)
			// The tier the config carried BEFORE this consent — the downgrade-
			// from-deep detection below needs the pre-change value.
			prevTier := cfg.Telemetry.EffectiveTier()
			// Downgrades take effect immediately AND purge staged rows the
			// new tier may not emit (plan §D2), even with the loop off.
			if err := applyTelemetryConsent(tier); err != nil {
				return err
			}
			// Deep consent must reach the server in the same act: the
			// receipt is the grant. Offline deep consent would create a
			// local tier the server refuses deep events for — fail loudly
			// instead, and roll the local tier back so config and server
			// agree.
			if tier == contract.TierDeep {
				if err := postDeepConsent(&cfg, http.MethodPost, ""); err != nil {
					_ = applyTelemetryConsent(contract.TierUsage)
					return fmt.Errorf("deep consent needs the telemetry endpoint to record the receipt: %w", err)
				}
			}
			// A downgrade FROM deep must also reach the server in the same
			// act: otherwise the server keeps a live receipt and admits deep
			// events the user just withdrawn consent for (live-caught in the
			// BR-10.7 wave). POST /v1/consent routes through RevokeDeep
			// server-side, purging deep raw; the explicit DELETE stays for
			// belt-and-braces only when the new tier is not a base tier.
			if prevTier == contract.TierDeep && tier != contract.TierDeep {
				if err := postDeepConsent(&cfg, http.MethodDelete, ""); err != nil {
					// Downgrade still stands locally (durable act), but the
					// user must know the server-side revocation failed.
					fmt.Fprintf(os.Stderr, "WARN: server-side deep revoke failed (%v) — deep data may remain until the 90-day sweep; retry with the telemetry endpoint reachable\n", err)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "server-side deep consent revoked; deep raw purged.")
				}
			}
			// Base-tier changes reach the server in the same act too (the
			// BR-10.7 lesson generalized): the consent act must reach the
			// holder. Raises fail loudly (rollback local); downgrades stand
			// locally with a loud WARN on server failure — user intent is
			// durable, the server side is eventually consistent via the
			// consent ceiling check on the next loop cycle.
			if tier != contract.TierDeep {
				if err := postBaseConsent(&cfg, tier); err != nil {
					if prevTier == contract.TierOff || tierRank(tier) > tierRank(prevTier) {
						_ = applyTelemetryConsent(prevTier)
						return fmt.Errorf("consent needs the telemetry endpoint to reach the server: %w", err)
					}
					fmt.Fprintf(os.Stderr, "WARN: server-side consent update failed (%v) — local tier stands; the server ceiling check re-syncs on the next cycle\n", err)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "consent tier set to %s.\n", tier)
			return nil
		},
	}
}

// applyTelemetryConsent persists the tier and purges now-inadmissible rows.
func applyTelemetryConsent(tier string) error {
	resolved, err := config.ResolveGlobalPath("")
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := config.Load(config.LoadOptions{GlobalPath: resolved})
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := config.SetConfigKey(&cfg, "telemetry.tier", tier); err != nil {
		return err
	}
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("refusing to save: %w", err)
	}
	if err := config.WriteToml(resolved, cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	// Purge on downgrade (fail-soft: the config change is the durable act).
	if tier == contract.TierOff || tier == contract.TierError || tier == contract.TierUsage {
		if database, derr := openTelemetryDB(resolved, cfg); derr == nil {
			c := telemetry.NewCollector(database, telemetry.CollectorConfig{Tier: tier})
			if _, perr := c.PurgeAboveTier(context.Background(), tier); perr != nil {
				fmt.Fprintf(os.Stderr, "WARN telemetry purge on downgrade: %v\n", perr)
			}
			_ = database.Close()
		}
	}
	return nil
}

func newTelemetryEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Enable the daemon-side collector loop (second key after consent)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setTelemetryEnabled(true)
		},
	}
}

func newTelemetryDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable the daemon-side collector loop",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setTelemetryEnabled(false)
		},
	}
}

func setTelemetryEnabled(enabled bool) error {
	resolved, err := config.ResolveGlobalPath("")
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := config.Load(config.LoadOptions{GlobalPath: resolved})
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := config.SetConfigKey(&cfg, "telemetry.enabled", fmt.Sprintf("%t", enabled)); err != nil {
		return err
	}
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("refusing to save: %w", err)
	}
	return config.WriteToml(resolved, cfg)
}

func newTelemetryRegisterCmd() *cobra.Command {
	var userID string
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register this install with the central ingest and store the token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// An empty user would be refused server-side anyway (the ingest
			// admits only users in its population — A2), and the refuse must be a
			// legible CLI error, not a 404 the loop would retry.
			if strings.TrimSpace(userID) == "" {
				return fmt.Errorf("register: --user is required — the central ingest admits only users it already knows")
			}
			cfg, err := config.Load(config.LoadOptions{GlobalPath: ""})
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			token, iid, err := telemetryRegister(cfg, userID)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "registered install %s; token stored (shown once): %s\n", iid, token)
			return nil
		},
	}
	cmd.Flags().StringVar(&userID, "user", "", "user id known to the central ingest (optional)")
	return cmd
}

// telemetryRegister mints an install + token via POST /v1/register and
// stores the token 0600. Idempotent: an existing token file short-circuits.
// On success the collector ADOPTS the server-issued install id (see
// adoptServerInstallID) — the wire identity must be the registered one.
func telemetryRegister(cfg config.Config, userID string) (token, installID string, err error) {
	if p := telemetryTokenPath(cfg); p != "" {
		if b, rerr := os.ReadFile(p); rerr == nil && len(strings.TrimSpace(string(b))) > 0 {
			return "", "", fmt.Errorf("already registered (token at %s); use reset-id to rotate", p)
		}
	}
	body, _ := json.Marshal(map[string]string{"user_id": userID})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.Telemetry.Endpoint, "/")+"/v1/register",
		strings.NewReader(string(body)))
	if err != nil {
		return "", "", fmt.Errorf("register: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient().Do(req)
	if err != nil {
		return "", "", fmt.Errorf("register: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		InstallID    string `json:"install_id"`
		InstallToken string `json:"install_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", fmt.Errorf("register: decode: %w", err)
	}
	if out.InstallID == "" || out.InstallToken == "" {
		return "", "", fmt.Errorf("register: server returned no identity (http %d)", resp.StatusCode)
	}
	if err := telemetryStoreToken(cfg, out.InstallToken); err != nil {
		return "", "", err
	}
	if err := adoptServerInstallID(cfg, out.InstallID); err != nil {
		return "", "", fmt.Errorf("register: adopt install id: %w", err)
	}
	return out.InstallToken, out.InstallID, nil
}

// adoptServerInstallID makes the collector use the server-issued install
// id. The ingest plane stores raw_events.install_id with a foreign key into
// ITS installs table, so only the REGISTERED identity is ever admitted; the
// locally minted one (anonymity window before register, plan §D2) exists
// only until this point. Adoption replaces it wholesale: the seq space
// restarts at 1 for the new identity and staged rows under the old identity
// are discarded — they were never sent, so nothing observable is lost.
func adoptServerInstallID(cfg config.Config, serverIID string) error {
	database, err := openTelemetryDB("", cfg)
	if err != nil {
		return err
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := database.ExecContext(ctx,
		`UPDATE telemetry_state SET install_id = ?, next_seq = 1, last_ack_seq = 0 WHERE id = 1`, serverIID); err != nil {
		return fmt.Errorf("update telemetry_state: %w", err)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM telemetry_staged`); err != nil {
		return fmt.Errorf("purge staged under prior identity: %w", err)
	}
	return nil
}

func newTelemetryResetIDCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset-id",
		Short: "Rotate the install id (orphans prior history; purges staged rows)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, database, cleanup, err := openTelemetryCollectorDB()
			if err != nil {
				return err
			}
			defer cleanup()
			c := telemetry.NewCollector(database, telemetry.CollectorConfig{
				Tier: cfg.Telemetry.EffectiveTier(),
			})
			// Cloud lane (no reporter install token): the install id is
			// derived from the device key, so rotating means a new key.
			if telemetryLoadToken(cfg) == "" {
				if !yes {
					return fmt.Errorf("on the cloud telemetry lane, reset-id revokes this machine's install and replaces its cloud device key, which also signs this machine out of cloud intelligence; re-run with --yes to confirm")
				}
				if err := resetCloudTelemetryID(cmd.Context(), cfg, c); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "install revoked server-side and the device key cleared; the next batch creates a new key and install id. Prior history is orphaned and ages out. Sign in to cloud intelligence again if you use it.")
				return nil
			}
			fresh, err := c.ResetInstallID(context.Background())
			if err != nil {
				return err
			}
			// Drop the stored token: the new identity is unregistered.
			if p := telemetryTokenPath(cfg); p != "" {
				_ = os.Remove(p)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "install id rotated to %s; prior history orphaned server-side. Run `observer telemetry register` to re-register.\n", fresh)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm a cloud-lane reset (replaces the device key)")
	return cmd
}

// --- shared plumbing -------------------------------------------------------

func httpClient() *http.Client { return &http.Client{Timeout: 30 * time.Second} }

// telemetryTokenPath resolves the install-token file path (relative paths
// resolve against the DB directory).
func telemetryTokenPath(cfg config.Config) string {
	name := cfg.Telemetry.InstallTokenFile
	if name == "" {
		name = "telemetry_install_token"
	}
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(filepath.Dir(cfg.Observer.DBPath), name)
}

// telemetryStoreToken writes the install token 0600.
func telemetryStoreToken(cfg config.Config, token string) error {
	p := telemetryTokenPath(cfg)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("telemetry token dir: %w", err)
	}
	if err := os.WriteFile(p, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("telemetry token write: %w", err)
	}
	return nil
}

// telemetryToken reads the stored install token.

// openTelemetryCollectorDB opens the daemon DB for a telemetry verb.
func openTelemetryCollectorDB() (config.Config, *sql.DB, func(), error) {
	cfg, err := config.Load(config.LoadOptions{GlobalPath: ""})
	if err != nil {
		return config.Config{}, nil, func() {}, fmt.Errorf("load config: %w", err)
	}
	database, err := openTelemetryDB("", cfg)
	if err != nil {
		return config.Config{}, nil, func() {}, err
	}
	return cfg, database, func() { _ = database.Close() }, nil
}

func openTelemetryDB(_ string, cfg config.Config) (*sql.DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	database, err := db.Open(ctx, db.Options{Path: cfg.Observer.DBPath})
	if err != nil {
		return nil, fmt.Errorf("open db %s: %w", cfg.Observer.DBPath, err)
	}
	return database, nil
}

// jitterInterval applies the plan's ±10% cadence jitter. math/rand is the
// right tool: jitter only spreads load, it is not a security decision
// (same posture as the orgclient backoff jitter).
func jitterInterval(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	//nolint:gosec // G404: cadence jitter, not crypto
	f := 0.9 + 0.2*rand.Float64()
	return time.Duration(float64(base) * f)
}

// postDeepConsent records the digest-bound deep consent receipt on the
// server (BR-10.7): POST /v1/consent/deep with the sha256 of the canonical
// disclosure text the user just confirmed. The server refuses any digest
// that does not bind the canonical text, so the receipt proves the user saw
// exactly this wording.
// postDeepConsent records or revokes the digest-bound deep consent on the
// server (BR-10.7): method POST grants (body carries the sha256 of the
// canonical disclosure text the user confirmed — the server refuses any
// other digest), method DELETE revokes (server marks receipts revoked,
// drops the tier to usage, purges deep raw immediately).
func postDeepConsent(cfg *config.Config, method, _ string) error {
	if cfg.Telemetry.Endpoint == "" {
		return fmt.Errorf("no telemetry endpoint configured")
	}
	if telemetryLoadToken(*cfg) == "" {
		// Cloud lane: deep is a tier on the install; revoking deep drops
		// to baseline, which purges deep-only events server-side.
		if method == http.MethodPost {
			return cloudConsentSync(*cfg, contract.TierDeep)
		}
		return cloudConsentSync(*cfg, contract.TierBaseline)
	}
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader(fmt.Sprintf(`{"disclosure_digest":%q}`, contract.DeepDisclosureDigest()))
	}
	req, err := http.NewRequest(method,
		trimRight(cfg.Telemetry.Endpoint, "/")+"/v1/consent/deep", body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := telemetryLoadToken(*cfg); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	return fmt.Errorf("server refused deep consent %s (%s): %s", method, resp.Status, strings.TrimSpace(string(data)))
}

// postBaseConsent records a base-tier consent change on the server
// (BR-10.7 generalization): POST /v1/consent {tier} for off/error/usage.
// Downgrades from deep are handled server-side by the same endpoint
// (SetBaseTier routes through RevokeDeep, purging deep raw).
func postBaseConsent(cfg *config.Config, tier string) error {
	if cfg.Telemetry.Endpoint == "" {
		return fmt.Errorf("no telemetry endpoint configured")
	}
	if telemetryLoadToken(*cfg) == "" {
		return cloudConsentSync(*cfg, tier)
	}
	body := fmt.Sprintf(`{"tier":%q}`, tier)
	req, err := http.NewRequest(http.MethodPost,
		trimRight(cfg.Telemetry.Endpoint, "/")+"/v1/consent", strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := telemetryLoadToken(*cfg); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	return fmt.Errorf("server refused consent %s (%s): %s", tier, resp.Status, strings.TrimSpace(string(data)))
}
