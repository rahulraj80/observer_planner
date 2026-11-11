//go:build !sbo_omit_terminals_sandbox_sandhold && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/keystore"
	"github.com/marmutapp/superbased-observer/internal/sandboxbackend"
	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
	"github.com/marmutapp/superbased-observer/internal/sandboxremote/sandhold"
	"github.com/marmutapp/superbased-observer/internal/sandboxselect"
)

// sandbox_sandhold.go is the SandHold row (SandHold notes 41 D2 and D3, 42):
// `observer sandbox connect <node-url>` signs this machine in to a SandHold
// node by the node's browser sign-in, `observer sandbox disconnect` revokes
// the key and forgets it, and the row runs a sandboxed terminal on that node
// as one more hosted vendor of the shared core (terminal_sandbox_hosted*.go):
// the same consent, upload, capture relay, teardown and leftover list as the
// other hosted rows, over the node's public API
// (internal/sandboxremote/sandhold Box). The key lives in the keystore (the
// OS keychain, or its hardened owner-only file fallback); the non-secret
// connection record is an owner-only state file, so the row is described
// without opening the keychain or calling the node (invariant I6).
// SANDHOLD_URL and SANDHOLD_API_KEY win over the saved sign-in (CI).

const sandholdComponent = "terminals.sandbox.sandhold"

// sandholdConnectHow is how this machine connects to a node.
const sandholdConnectHow = "connect with `observer sandbox connect <node-url>`, or set " + sandhold.EnvURL + " and " + sandhold.EnvKey

// The row's own probe verdicts (copy keys); a missing connection is the
// shared credential_missing.
const (
	verdictSandholdNotConnected = "credential_missing" //nolint:gosec // G101: a verdict copy key, not a credential
	verdictSandholdKeyExpired   = "credential_expired" //nolint:gosec // G101: a verdict copy key, not a credential
)

func init() {
	// The row's provider table: its template and box_binary from
	// [terminal.sandbox.hosted.sandhold], its node from the connection
	// (or SANDHOLD_URL), never a region or an endpoint from a file.
	hostedVendorConfigTable = append(hostedVendorConfigTable, hostedVendorConfigRow{
		vendor: sandboxremote.VendorSandHold,
		read: func(c config.TerminalSandboxHostedConfig) config.TerminalSandboxHostedProviderConfig {
			pc := c.SandHold
			pc.Region, pc.EndpointSPKISHA256, pc.OrgPublished, pc.BoxBinaryInImage = "", "", false, false
			pc.Endpoint = sandholdNode(sandholdNodeDir())
			return pc
		},
	})
	registerHostedVendor(hostedVendorReg{
		vendor: sandboxremote.VendorSandHold, component: sandholdComponent,
		newClient: func(cred sandboxremote.Credential, endpoint string) (sandboxremote.Client, error) {
			// The key goes only to the node it is bound to (the core
			// checked the binding before this).
			return sandhold.NewBox(endpoint, cred.Get(sandhold.EnvKey))
		},
		localProbe:       sandholdLocalProbe,
		credential:       sandholdHostedCredential,
		credentialSource: sandholdHostedCredentialSource,
		connectHow:       sandholdConnectHow,
		built:            func(r *sandboxRuntime) { sandholdSetNodeDir(r.observerDir) },
		probeHint: func(d *hostedDriver) string {
			return sandholdProbeAt(d.r.observerDir, d.now()).Hint
		},
		// While the component is not selected the row still says whether
		// this machine is connected: it reads only its own state file.
		inactiveHint: func(r *sandboxRuntime, _ config.Config) string {
			return sandholdProbeAt(r.observerDir, time.Now()).Hint
		},
	})
	sandboxSubcommands = append(sandboxSubcommands, newSandboxConnectCmd, newSandboxDisconnectCmd)
}

// sandholdLookupEnv reads SANDHOLD_URL and SANDHOLD_API_KEY (a variable for
// tests).
var sandholdLookupEnv = os.LookupEnv

// sandholdDirMu guards sandholdDirSet, the observer dir the row's node is
// read from: the built driver's, else the default ~/.observer (the CLI verbs
// that list or remove leftover boxes run without a driver).
var (
	sandholdDirMu  sync.Mutex
	sandholdDirSet string
)

func sandholdSetNodeDir(dir string) {
	sandholdDirMu.Lock()
	sandholdDirSet = dir
	sandholdDirMu.Unlock()
}

func sandholdNodeDir() string {
	sandholdDirMu.Lock()
	dir := sandholdDirSet
	sandholdDirMu.Unlock()
	if dir == "" {
		dir, _ = hostedObserverDir()
	}
	return dir
}

// sandholdEnv reads one of the row's variables, trimmed.
func sandholdEnv(name string) string {
	v, _ := sandholdLookupEnv(name)
	return strings.TrimSpace(v)
}

// sandholdNode is the node a launch would use, from local facts only:
// SANDHOLD_URL, else the node of the saved connection; "" for none. An
// address NodeURL refuses is returned as it is, so the row says why.
func sandholdNode(observerDir string) string {
	if raw := sandholdEnv(sandhold.EnvURL); raw != "" {
		if u, err := sandhold.NodeURL(raw); err == nil {
			return u
		}
		return raw
	}
	if conn, err := sandholdLoadConnection(observerDir); err == nil && conn != nil {
		return conn.URL
	}
	return ""
}

// sandholdHostedCredentialSource says where the key would come from without
// reading it (sandhold.Resolve's rules over local facts): "env" for
// SANDHOLD_API_KEY with a node, "keystore" for a saved sign-in to the node
// in use, "" for none.
func sandholdHostedCredentialSource(observerDir string) string {
	conn, err := sandholdLoadConnection(observerDir)
	if err != nil {
		conn = nil
	}
	envURL := sandholdEnv(sandhold.EnvURL)
	if sandholdEnv(sandhold.EnvKey) != "" && (envURL != "" || conn != nil) {
		return sandboxremote.SourceEnv
	}
	if conn == nil {
		return ""
	}
	if envURL != "" {
		if u, err := sandhold.NodeURL(envURL); err != nil || u != conn.URL {
			return "" // the saved key belongs to the saved node only
		}
	}
	return sandboxremote.SourceKeystore
}

// sandholdHostedCredential reads the key at use time (a launch, the leftover
// list's confirm): sandhold.Resolve over the saved sign-in and the
// environment, bound to the node it belongs to.
func sandholdHostedCredential(observerDir string) (sandboxremote.Credential, error) {
	saved, err := sandholdLoadCredential(observerDir)
	if err != nil && sandholdEnv(sandhold.EnvKey) == "" {
		return sandboxremote.Credential{}, fmt.Errorf("%w: %w", sandboxremote.ErrCredentialMissing, err)
	}
	s, err := sandhold.Resolve(saved, sandholdLookupEnv)
	if err != nil {
		return sandboxremote.Credential{}, fmt.Errorf("%w: %w", sandboxremote.ErrCredentialMissing, err)
	}
	if s.Key == "" || s.URL == "" {
		return sandboxremote.Credential{}, fmt.Errorf("%w: this machine holds no key for a SandHold node; %s", sandboxremote.ErrCredentialMissing, sandholdConnectHow)
	}
	src := sandboxremote.SourceKeystore
	if s.FromEnv {
		src = sandboxremote.SourceEnv
	}
	return sandboxremote.Credential{Provider: sandboxremote.VendorSandHold, Fields: map[string]string{sandhold.EnvKey: s.Key}, Endpoint: s.URL, Source: src}, nil
}

// sandholdLocalProbe is the row's own probe rows, before the shared ladder:
// a connection record that cannot be read, and a saved key past its expiry
// (a CI key in the environment wins over both). Local facts only.
func sandholdLocalProbe(d *hostedDriver) (string, string) {
	if sandholdHostedCredentialSource(d.r.observerDir) == sandboxremote.SourceEnv {
		return "", ""
	}
	p := sandholdProbeAt(d.r.observerDir, d.now())
	if p.Verdict == verdictSandholdKeyExpired || (p.Verdict == verdictSandholdNotConnected && p.Reason != sandhold.NotConnected) {
		return p.Verdict, p.Reason
	}
	return "", ""
}

// sandholdProbeAt is the row's connection state over observerDir.
func sandholdProbeAt(observerDir string, now time.Time) sandboxselect.Probe {
	conn, err := sandholdLoadConnection(observerDir)
	return sandholdProbeFor(conn, err, sandholdEnvKeySet(), now)
}

// sandholdEnvKeySet reports a CI key in the environment (it wins over a
// saved sign-in, note 41 D3).
func sandholdEnvKeySet() bool { return sandholdEnv(sandhold.EnvKey) != "" }

// sandholdProbeFor says the connection's state from local facts, top-down:
// its Verdict is "" when the connection is usable (the shared ladder then
// decides), and its Hint is the row's note.
func sandholdProbeFor(conn *sandhold.Connection, readErr error, envKey bool, now time.Time) sandboxselect.Probe {
	p := sandboxselect.Probe{Compiled: true, Version: "1", Sources: []string{"live"}}
	switch {
	case readErr != nil:
		p.Verdict = verdictSandholdNotConnected
		p.Reason = "the SandHold connection record could not be read (" + readErr.Error() + "); connect again: observer sandbox connect <node-url>"
		p.Hint = p.Reason
	case conn == nil && envKey:
		p.Hint = "a key is set in " + sandhold.EnvKey + " for the node in " + sandhold.EnvURL + " (it wins over a saved sign-in; not checked here, the probe never calls the node)"
	case conn == nil:
		p.Verdict, p.Reason = verdictSandholdNotConnected, sandhold.NotConnected
		p.Hint = sandhold.NotConnected
	case conn.Expired(now):
		p.Verdict = verdictSandholdKeyExpired
		p.Reason = "the key for " + conn.URL + " expired on " + conn.ExpiresAt.UTC().Format("2 Jan 2006") + "; connect again: observer sandbox connect " + conn.URL
		p.Hint = conn.Summary(now)
	default:
		p.Hint = conn.Summary(now)
		if envKey {
			p.Hint += "; " + sandhold.EnvKey + " is set and wins over this sign-in"
		}
	}
	return p
}

// ---- where the connection is kept -------------------------------------------

// sandholdKeystoreOpen opens the key's store (a variable so tests use a
// file store in a temporary directory, never the OS keychain).
var sandholdKeystoreOpen = func(observerDir string) keystore.Store {
	return hostedKeystoreOpen(sandhold.KeystoreService, hostedCredDir(observerDir))
}

// sandholdDir is the row's state directory.
func sandholdDir(observerDir string) string {
	return hostedProviderDir(observerDir, sandboxbackend.BackendSandHold)
}

// sandholdConnectionPath is the non-secret connection record.
func sandholdConnectionPath(observerDir string) string {
	return filepath.Join(sandholdDir(observerDir), "connection.json")
}

// sandholdLoadConnection reads the connection record: nil and no error when
// this machine is not connected.
func sandholdLoadConnection(observerDir string) (*sandhold.Connection, error) {
	b, err := hostedReadOwned(sandholdConnectionPath(observerDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c sandhold.Connection
	if err := json.Unmarshal(b, &c); err != nil || c.Version != 1 || c.URL == "" {
		return nil, errors.New("the record does not parse")
	}
	return &c, nil
}

// sandholdSaveConnection writes the connection record (owner only, a fresh
// temporary file renamed over the old one).
func sandholdSaveConnection(observerDir string, c sandhold.Connection) error {
	c.Version = 1
	return hostedWithState(observerDir, true, func() error {
		return hostedWriteOwned(sandholdConnectionPath(observerDir), c)
	})
}

// sandholdLoadCredential reads the saved key: nil and no error when none is
// saved.
func sandholdLoadCredential(observerDir string) (*sandhold.Credential, error) {
	raw, err := sandholdKeystoreOpen(observerDir).Get(sandhold.KeystoreRecord)
	if errors.Is(err, keystore.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("the SandHold key could not be read from the keystore: %w", err)
	}
	c, err := sandhold.DecodeCredential([]byte(raw))
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// sandholdSaveCredential stores the key in the keystore and says where, in
// plain words.
func sandholdSaveCredential(observerDir string, c sandhold.Credential) (string, error) {
	b, err := sandhold.EncodeCredential(c)
	if err != nil {
		return "", err
	}
	ks := sandholdKeystoreOpen(observerDir)
	if err := ks.Set(sandhold.KeystoreRecord, string(b)); err != nil {
		return "", fmt.Errorf("the SandHold key could not be stored: %w", err)
	}
	if ks.Backend() == "keychain" {
		return "this machine's keychain (service " + sandhold.KeystoreService + ")", nil
	}
	return "an owner-only file under " + hostedCredDir(observerDir) + " (this machine has no usable keychain)", nil
}

// sandholdForget removes the key and the connection record. A key already
// gone still removes the record, so the row never reads as connected with
// nothing behind it.
func sandholdForget(observerDir string) error {
	if err := sandholdKeystoreOpen(observerDir).Delete(sandhold.KeystoreRecord); err != nil && !errors.Is(err, keystore.ErrNotFound) {
		return fmt.Errorf("the SandHold key could not be removed from the keystore: %w", err)
	}
	return hostedWithState(observerDir, false, func() error {
		err := os.Remove(sandholdConnectionPath(observerDir))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	})
}

// ---- observer sandbox connect / disconnect ----------------------------------

// sandholdDeps are the seams of the two commands (tests set them).
type sandholdDeps struct {
	observerDir string
	openBrowser func(string)
	sleep       func(ctx context.Context, d time.Duration) error
	now         func() time.Time
	hostname    func() (string, error)
	lookupEnv   func(string) (string, bool)
}

func sandholdDefaultDeps(cfgPath string) (sandholdDeps, error) {
	cfg, err := config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if err != nil {
		return sandholdDeps{}, fmt.Errorf("load config: %w", err)
	}
	return sandholdDeps{
		observerDir: sandboxObserverDirFor(cfg),
		openBrowser: openBrowser, now: time.Now, hostname: os.Hostname, lookupEnv: os.LookupEnv,
	}, nil
}

func newSandboxConnectCmd() *cobra.Command {
	var (
		cfgPath   string
		noBrowser bool
	)
	cmd := &cobra.Command{
		Use:   "connect [<node-url>]",
		Short: "Connect this machine to a SandHold node: approve it in the node's portal, and Observer keeps the key",
		Long: "Connect this machine to a SandHold node. Observer asks the node for a sign-in code, shows you the node's approval page\n" +
			"and the code (and opens the page when it is on the node you named), and waits while you approve this machine there.\n" +
			"The key the node then gives is kept in this machine's keychain and sent only to that node. Observer prints who you\n" +
			"are signed in as and which isolation the node offers. SANDHOLD_URL and SANDHOLD_API_KEY, when set, win over it (CI).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deps, err := sandholdDefaultDeps(cfgPath)
			if err != nil {
				return fmt.Errorf("sandbox connect: %w", err)
			}
			raw := ""
			if len(args) == 1 {
				raw = args[0]
			}
			return runSandholdConnect(cmd.Context(), cmd.OutOrStdout(), deps, raw, noBrowser)
		},
	}
	cmd.Flags().StringVar(&cfgPath, "config", "", "config file (default ~/.observer/config.toml)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the approval page's address without opening a browser")
	return cmd
}

func newSandboxDisconnectCmd() *cobra.Command {
	var cfgPath string
	cmd := &cobra.Command{
		Use:   "disconnect",
		Short: "Revoke the key this machine holds for its SandHold node and forget the connection",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			deps, err := sandholdDefaultDeps(cfgPath)
			if err != nil {
				return fmt.Errorf("sandbox disconnect: %w", err)
			}
			return runSandholdDisconnect(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
	cmd.Flags().StringVar(&cfgPath, "config", "", "config file (default ~/.observer/config.toml)")
	return cmd
}

// sandholdBrowserSafe is the shape of a page address the browser opener may
// be given (on Windows it passes through cmd's start): letters, digits and
// URL punctuation only, no quote, ampersand, percent, caret, pipe or
// bracket of a shell or cmd. The node's page (/app/#/device?code=XXXX-XXXX)
// fits; anything else is printed for the person to open.
var sandholdBrowserSafe = regexp.MustCompile(`^https?://[A-Za-z0-9.\-:\[\]]+(/[A-Za-z0-9/#?=._~+@:\-]*)?$`)

// sandholdClientID is what the machine calls itself on the approval page.
func sandholdClientID(hostname func() (string, error)) string {
	id := "Observer"
	if hostname != nil {
		if h, err := hostname(); err == nil && strings.TrimSpace(h) != "" {
			id += " on " + sandhold.PlainLine(h)
		}
	}
	if len(id) > 60 {
		id = id[:60]
	}
	return id
}

// runSandholdConnect is `observer sandbox connect`.
func runSandholdConnect(ctx context.Context, w io.Writer, deps sandholdDeps, raw string, noBrowser bool) error {
	out := sandhold.PlainWriter(w)
	if deps.now == nil {
		deps.now = time.Now
	}
	saved, savedErr := sandholdLoadCredential(deps.observerDir)
	if raw == "" && deps.lookupEnv != nil {
		raw, _ = deps.lookupEnv(sandhold.EnvURL)
	}
	if raw == "" && saved != nil {
		raw = saved.URL
	}
	if strings.TrimSpace(raw) == "" {
		return errors.New("sandbox connect: which node? observer sandbox connect <node-url>, for example: observer sandbox connect https://sandhold.example.com")
	}
	node, err := sandhold.NodeURL(raw)
	if err != nil {
		return fmt.Errorf("sandbox connect: %w", err)
	}
	si := &sandhold.SignIn{
		Client:   sandhold.NewClient(node, ""),
		ClientID: sandholdClientID(deps.hostname),
		Sleep:    deps.sleep, Now: deps.now,
		Shown: func(s sandhold.Start, sameNode bool) {
			fmt.Fprintf(out, "To connect, approve this machine in your browser:\n\n  %s\n\nCode: %s   (the page must show this same code)\n\n", s.Page(), sandhold.PlainLine(s.UserCode))
			switch {
			case !sameNode:
				fmt.Fprintf(out, "Note: that page is not under %s, the address you gave. Open it only if it is your node.\n", node)
			case !noBrowser && deps.openBrowser != nil && sandholdBrowserSafe.MatchString(s.Page()):
				deps.openBrowser(s.Page())
				fmt.Fprintln(out, "Your browser should open that page; if it does not, open it yourself.")
			}
			mins := s.ExpiresIn / 60
			if mins <= 0 {
				mins = 10
			}
			fmt.Fprintf(out, "Waiting for your approval (the code works for %d minutes; Ctrl-C to stop)...\n", mins)
		},
	}
	g, err := si.Run(ctx)
	switch {
	case errors.Is(err, sandhold.ErrNoBrowserSignIn):
		return fmt.Errorf("sandbox connect: %s has no browser sign-in (it is switched off there, or the node is older); to fix: ask the node's operator, or use the node's own `sandhold login --key-stdin`", node)
	case errors.Is(err, sandhold.ErrDenied):
		return fmt.Errorf("sandbox connect: not connected: %w", err)
	case errors.Is(err, sandhold.ErrExpired):
		return fmt.Errorf("sandbox connect: not connected: %w; to fix: run observer sandbox connect %s again", err, node)
	case err != nil:
		return fmt.Errorf("sandbox connect: %w", err)
	}

	c := sandhold.NewClient(node, g.Key)
	conn := sandhold.Connection{URL: node, KeyID: g.KeyID, Org: g.Org, User: g.User, ExpiresAt: g.ExpiresAt, ConnectedAt: deps.now().UTC()}
	where, err := sandholdSaveCredential(deps.observerDir, sandhold.Credential{URL: node, Key: g.Key, KeyID: g.KeyID})
	if err != nil {
		// Never leave a key working that this machine cannot keep.
		if rerr := c.RevokeKey(ctx, g.KeyID); rerr != nil {
			return fmt.Errorf("sandbox connect: %w; the new key could not be revoked either (%v): revoke it in the node's portal, %s/app (API access)", err, rerr, node)
		}
		return fmt.Errorf("sandbox connect: %w (the new key was revoked at the node)", err)
	}
	conn.KeyStore = where
	var whoErr error
	if who, err := c.Whoami(ctx); err == nil {
		conn.Org, conn.User = firstNonEmpty(who.Org, conn.Org), firstNonEmpty(who.User, conn.User)
		conn.KeyID = firstNonEmpty(conn.KeyID, who.KeyID)
	} else {
		whoErr = err
	}
	targets, terr := c.Targets(ctx)
	if terr == nil {
		conn.Offers = sandhold.Offers(targets)
	} else {
		conn.TargetsError = sandhold.PlainLine(terr.Error())
	}
	if err := sandholdSaveConnection(deps.observerDir, conn); err != nil {
		return fmt.Errorf("sandbox connect: the key is kept, but the connection record could not be written: %w", err)
	}
	// A new sign-in revokes the key it replaces, sent only to the node that
	// issued it. Best effort: the old node may be gone.
	if savedErr == nil && saved != nil && saved.Key != g.Key && saved.KeyID != "" {
		old := sandhold.NewClient(saved.URL, saved.Key)
		if err := old.RevokeKey(ctx, saved.KeyID); err != nil {
			fmt.Fprintf(out, "Note: the key this machine held for %s could not be revoked (%s); revoke it in that node's portal under API access.\n", saved.URL, sandhold.PlainLine(err.Error()))
		}
	}

	fmt.Fprintf(out, "\nConnected to %s", node)
	if conn.User != "" {
		fmt.Fprintf(out, " as %s", conn.User)
		if conn.Org != "" {
			fmt.Fprintf(out, " in %s", conn.Org)
		}
	}
	fmt.Fprintln(out, ".")
	fmt.Fprintf(out, "  The key is kept in %s and sent only to %s", where, node)
	if !g.ExpiresAt.IsZero() {
		fmt.Fprintf(out, "; it works until %s", g.ExpiresAt.UTC().Format("2 Jan 2006"))
	}
	fmt.Fprintln(out, ".")
	fmt.Fprintln(out, "  Disconnect any time with: observer sandbox disconnect (it revokes the key at the node).")
	if whoErr != nil {
		fmt.Fprintf(out, "  Note: the node did not say who the key belongs to (%s).\n", sandhold.PlainLine(whoErr.Error()))
	}
	fmt.Fprintln(out)
	switch {
	case terr != nil:
		fmt.Fprintf(out, "The isolation this node offers could not be read: %s\n", conn.TargetsError)
	case len(conn.Offers) == 0:
		fmt.Fprintln(out, "This node offers you no isolation right now (no target is active for you); ask its operator.")
	default:
		fmt.Fprintln(out, "Isolation this node offers you:")
		for _, o := range conn.Offers {
			fmt.Fprintf(out, "  %s\n", o.Line())
		}
	}
	fmt.Fprintln(out, "\nThe node appears in Observer's sandbox backends as \"sandhold\" (observer sandbox status --backend sandhold).")
	fmt.Fprintln(out, "To run a sandboxed terminal on it, add \"sandhold\" to [terminal.sandbox].backends and choose it by name; the first run asks you to agree to what is sent to the node.")
	return nil
}

// runSandholdDisconnect is `observer sandbox disconnect`.
func runSandholdDisconnect(ctx context.Context, w io.Writer, deps sandholdDeps) error {
	out := sandhold.PlainWriter(w)
	saved, err := sandholdLoadCredential(deps.observerDir)
	conn, _ := sandholdLoadConnection(deps.observerDir)
	if err != nil {
		// A key that cannot be read cannot be revoked from here: forget it
		// and say where to revoke it.
		if ferr := sandholdForget(deps.observerDir); ferr != nil {
			return fmt.Errorf("sandbox disconnect: %w", ferr)
		}
		node := "the node"
		if conn != nil {
			node = conn.URL
		}
		fmt.Fprintf(out, "The saved key could not be read (%s), so it was not revoked; it is removed from this machine.\n  To fix: revoke it in %s's portal under API access.\n", sandhold.PlainLine(err.Error()), node)
		return nil
	}
	if saved == nil {
		if conn != nil {
			if err := sandholdForget(deps.observerDir); err != nil {
				return fmt.Errorf("sandbox disconnect: %w", err)
			}
			fmt.Fprintf(out, "No key was kept for %s; its connection record is removed.\n", conn.URL)
			return nil
		}
		fmt.Fprintln(out, "This machine is not connected to a SandHold node.")
		return nil
	}
	revoked := "The key could not be revoked at the node"
	ok := false
	if saved.KeyID == "" {
		revoked += " (no key id was saved with it)"
	} else if err := sandhold.NewClient(saved.URL, saved.Key).RevokeKey(ctx, saved.KeyID); err != nil {
		revoked += " (" + sandhold.PlainLine(err.Error()) + ")"
	} else {
		revoked, ok = "The key is revoked at the node", true
	}
	if err := sandholdForget(deps.observerDir); err != nil {
		return fmt.Errorf("sandbox disconnect: %s, but it could not be removed from this machine: %w", revoked, err)
	}
	fmt.Fprintf(out, "Disconnected from %s. %s and removed from this machine.\n", saved.URL, revoked)
	if !ok {
		fmt.Fprintf(out, "  To fix: revoke it in the node's portal: %s/app (API access)\n", saved.URL)
	}
	return nil
}
