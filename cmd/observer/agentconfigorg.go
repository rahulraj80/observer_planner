package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/agentconfigorg"
	"github.com/marmutapp/superbased-observer/internal/agentconfigsvc"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/hook"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// The org AI tool policy on a managed node (adapter config P6/P7): the CLI
// twin of the dashboard's Organization tab (`observer agent-config org
// status|apply`), and the daemon's apply loop.

func newAgentConfigOrgCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Show or apply your organization's AI tool policy on this managed machine",
	}
	var asJSON bool
	status := &cobra.Command{
		Use:   "status",
		Short: "The org AI tool policy this machine holds and what Observer did with each setting",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := openAgentConfig(cmd.Context(), *configPath)
			if err != nil {
				return err
			}
			defer env.cleanup()
			return printAgentConfigOrgStatus(cmd.Context(), cmd.OutOrStdout(), env, asJSON)
		},
	}
	status.Flags().BoolVar(&asJSON, "json", false, "Print JSON")
	apply := &cobra.Command{
		Use:   "apply",
		Short: "Apply the org AI tool policy now (the daemon also applies it on every change and once a minute)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := openAgentConfig(cmd.Context(), *configPath)
			if err != nil {
				return err
			}
			defer env.cleanup()
			if env.st == nil || !env.managed {
				return errors.New("this machine is not managed by an organization, so there is no org AI tool policy to apply")
			}
			res, err := env.orgSvc.SyncOrg(cmd.Context(), env.st)
			if err != nil {
				return err
			}
			if !res.HasPolicy {
				return errors.New("your organization has not sent an AI tool policy to this machine yet")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Applied org AI tool policy version %d: %d file write(s), %d target(s), %d new drift event(s).\n",
				res.Version, res.Writes, res.Targets, res.DriftEvents)
			return nil
		},
	}
	cmd.AddCommand(status, apply)
	return cmd
}

// agentConfigOrgStatus is the JSON shape of `org status`.
type agentConfigOrgStatus struct {
	Managed         bool                               `json:"managed"`
	HasPolicy       bool                               `json:"has_policy"`
	Version         int64                              `json:"version,omitempty"`
	OnDrift         string                             `json:"on_drift,omitempty"`
	DeveloperAccess string                             `json:"developer_access,omitempty"`
	SecretsPosture  string                             `json:"secrets_posture,omitempty"`
	Grants          []string                           `json:"grants"`
	Locked          map[string]string                  `json:"locked"`
	Rows            []agentConfigOrgStatusRow          `json:"rows"`
	Directives      []orgcontract.AgentConfigDirective `json:"directives"`
}

type agentConfigOrgStatusRow struct {
	Setting     string `json:"setting"`
	FileID      string `json:"file_id"`
	ProjectRoot string `json:"project_root,omitempty"`
	Mode        string `json:"mode"`
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
	Policy      string `json:"policy"`
	DriftCount  int    `json:"drift_count"`
}

func printAgentConfigOrgStatus(ctx context.Context, out io.Writer, env agentConfigEnv, asJSON bool) error {
	st := agentConfigOrgStatus{Managed: env.managed, Grants: []string{}, Locked: map[string]string{}, Rows: []agentConfigOrgStatusRow{}, Directives: []orgcontract.AgentConfigDirective{}}
	if env.st != nil {
		if body, row, ok := env.st.OrgAgentConfigBody(ctx); ok {
			st.HasPolicy, st.Version = true, row.Version
			st.OnDrift = orgcontract.NormalizeAgentConfigOnDrift(body.OnDrift)
			st.DeveloperAccess = orgcontract.NormalizeAgentConfigAccess(body.DeveloperAccess)
			st.SecretsPosture = orgcontract.NormalizeAgentConfigSecrets(body.SecretsPosture)
			for _, g := range body.Grants {
				st.Grants = append(st.Grants, g.Setting)
			}
			st.Directives = append(st.Directives, body.Directives...)
		}
		rows, err := env.st.CurrentAgentConfigOrgState(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			st.Rows = append(st.Rows, agentConfigOrgStatusRow{
				Setting: r.SettingID, FileID: r.FileID, ProjectRoot: r.ProjectRoot, Mode: r.Mode, State: r.State,
				Reason: r.Reason, Policy: r.PolicyName, DriftCount: len(r.DriftTimes),
			})
		}
	}
	for k, v := range env.access.Locked {
		st.Locked[k] = v
	}
	if asJSON {
		return printJSON(out, st)
	}
	if !st.Managed {
		fmt.Fprintln(out, "This machine is not managed by an organization: no org AI tool policy applies.")
		return nil
	}
	if !st.HasPolicy {
		fmt.Fprintln(out, "Your organization has not sent an AI tool policy to this machine yet. AI tool settings are read-only until it does.")
		return nil
	}
	grants := "none"
	if len(st.Grants) > 0 {
		grants = strings.Join(st.Grants, ", ")
	}
	fmt.Fprintf(out, "Org AI tool policy version %d\nOn drift: %s   Developer access: %s   Plaintext secrets: %s\nEditing granted for: %s\n\n",
		st.Version, st.OnDrift, strings.ReplaceAll(st.DeveloperAccess, "_", "-"), st.SecretsPosture, grants)
	if len(st.Rows) == 0 {
		fmt.Fprintln(out, "No setting has been applied yet (the daemon applies the policy within a minute, or run: observer agent-config org apply).")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "SETTING\tMODE\tSTATE\tFILE\tPROJECT\tPOLICY\tDRIFTS")
	for _, r := range st.Rows {
		state := strings.ReplaceAll(r.State, "_", " ")
		if r.Reason != "" {
			state += " (" + r.Reason + ")"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n", r.Setting, r.Mode, state, r.FileID, r.ProjectRoot, r.Policy, r.DriftCount)
	}
	return tw.Flush()
}

// agentConfigOrgApplier is the daemon-lifetime apply loop: a verified policy
// change (the org client's sink) kicks it, and a one-minute sweep catches a
// hand edit between changes (lever B drift detection, D-100/D-101).
type agentConfigOrgApplier struct {
	st     *store.Store
	svc    *agentconfigsvc.Service
	gate   func(context.Context) bool
	kick   chan struct{}
	logger *slog.Logger
	once   sync.Once
	// lastInventory is when the AI tool configuration inventory (D-112,
	// T-427) was last built; only the loop goroutine touches it.
	lastInventory time.Time
}

// agentConfigOrgSweep is the drift sweep interval.
const agentConfigOrgSweep = time.Minute

// agentConfigInventoryEvery is how often a managed node rebuilds its AI tool
// configuration inventory (it reads every installed tool's files). The push
// seam ships it only when it changed.
const agentConfigInventoryEvery = 10 * time.Minute

func newAgentConfigOrgApplier(st *store.Store, cfg config.Config, gate func(context.Context) bool, logger *slog.Logger) *agentConfigOrgApplier {
	captured := map[string]bool{}
	for _, a := range cfg.Observer.Watch.EnabledAdapters {
		captured[a] = true
	}
	port := cfg.Proxy.Port
	return &agentConfigOrgApplier{
		st:   st,
		gate: gate,
		kick: make(chan struct{}, 1),
		svc: agentconfigsvc.New(agentconfigsvc.Options{
			Store: st,
			Lock:  hook.LockSettingsFile,
			ProxyAddrs: []string{
				fmt.Sprintf("127.0.0.1:%d", port), fmt.Sprintf("localhost:%d", port), fmt.Sprintf("[::1]:%d", port),
			},
			Captured:   func(tool string) bool { return captured[tool] },
			EditGrants: cliConfigEditGrants(st),
			Origin:     "org",
		}),
		logger: logger,
	}
}

// Kick asks for an apply now (non-blocking, coalesced).
func (a *agentConfigOrgApplier) Kick() {
	if a == nil {
		return
	}
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

// Start runs the loop until ctx ends (idempotent).
func (a *agentConfigOrgApplier) Start(ctx context.Context) {
	if a == nil {
		return
	}
	a.once.Do(func() {
		go func() {
			t := time.NewTicker(agentConfigOrgSweep)
			defer t.Stop()
			a.applyOnce(ctx)
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				case <-a.kick:
				}
				a.applyOnce(ctx)
			}
		}()
	})
}

func (a *agentConfigOrgApplier) applyOnce(ctx context.Context) {
	if a.gate == nil || !a.gate(ctx) {
		return
	}
	res, err := a.svc.SyncOrg(ctx, a.st)
	if err != nil {
		a.logger.Warn("agent-config org apply failed", "err", err)
	} else if res.Writes > 0 || res.DriftEvents > 0 {
		a.logger.Info("agent-config org policy applied", "version", res.Version, "writes", res.Writes, "drift_events", res.DriftEvents)
	}
	a.inventoryOnce(ctx, time.Now())
}

// inventoryOnce rebuilds the AI tool configuration inventory (D-112: the org
// sees everything on a managed node, T-427) at most every
// agentConfigInventoryEvery, after the apply so it reads the files as the
// apply left them. It needs no policy: the managed gate is enough.
func (a *agentConfigOrgApplier) inventoryOnce(ctx context.Context, now time.Time) {
	if !a.lastInventory.IsZero() && now.Sub(a.lastInventory) < agentConfigInventoryEvery {
		return
	}
	roots, err := a.st.ProjectRootsByRemoteHash(ctx)
	if err != nil {
		a.logger.Warn("agent-config inventory: project roots", "err", err)
		return
	}
	a.lastInventory = now
	a.st.SetAgentConfigInventory(a.svc.Inventory(ctx, roots))
}

// agentConfigDriftRisk adds the closed capture-risk label when an enforced
// org setting no longer holds on this node (a drift reported or backed off).
// The detail rides PushEnvelope.AgentConfigState.
func agentConfigDriftRisk(ctx context.Context, st *store.Store, report *orgcontract.ManagedIntegrityReport) {
	if st == nil || report == nil {
		return
	}
	rows, err := st.CurrentAgentConfigOrgState(ctx)
	if err != nil {
		return
	}
	for _, r := range rows {
		s := agentconfigorg.State(r.State)
		if r.Mode == orgcontract.AgentConfigModeEnforced && (s == agentconfigorg.StateDrifted || s == agentconfigorg.StateBackedOff) {
			for _, l := range report.CaptureRisks {
				if l == orgcontract.CaptureRiskAgentConfigDrift {
					return
				}
			}
			report.CaptureRisks = append(report.CaptureRisks, orgcontract.CaptureRiskAgentConfigDrift)
			sort.Strings(report.CaptureRisks)
			return
		}
	}
}
