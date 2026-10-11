package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/componentsvc"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// components_gate.go is the daemon and CLI side of the L2 "installed but
// inert" gate (modular packaging plan section 4, phase P1, lane MP-GATE).
// Every daemon goroutine, builder and CLI verb of an optional component
// consults ONE predicate, componentstate.Snapshot.Active, through the data
// tables below. An install with no [components] block resolves every
// compiled component active, so nothing here changes what it runs.

// daemonLoopComponents maps every named `observer start` goroutine
// (namedGroup.Go names in start.go) to the components it serves. An empty
// list is core: it always runs. A loop serving several components runs
// when any of them is active. TestEveryDaemonLoopIsClassified keeps the
// table in step with start.go.
var daemonLoopComponents = map[string][]string{
	"watcher":          nil,
	"surface-enricher": nil,
	"spend-verdicts":   nil,
	"db-maintenance":   nil,
	"retention":        nil,
	"wal-watchdog":     nil,
	"temp-watchdog":    nil,
	// The lifecycle hook spool drainer ingests rows hooks already wrote; a
	// spool left behind by hooks that were since removed still ingests.
	"hook-lifecycle": nil,

	"proxy":               {"capture.proxy"},
	"dashboard":           {"ui.dashboard"},
	"guidance-scan":       {"analytics"},
	"session-score":       {"analytics"},
	"advisor-digest":      {"analytics"},
	"commit-scan":         {"projects"},
	"codeintel":           {"codeintel"},
	"cloud-auto-sync":     {"cloud.intelligence"},
	"cloud-auto-enrich":   {"cloud.intelligence"},
	"pricing-auto-sync":   {"egress.pricing-feed"},
	"attach-socket":       {"terminals"},
	"tailnet-backend":     {"remote"},
	"mcp-relay":           {"agentaccess.relay"},
	"guard-cloud":         {"guard.cloud"},
	"otel-exporter":       {"export.otel"},
	"guard-otel-tail":     {"export.otel"},
	"otlp-receiver":       {"ingest.otlp", "obs"},
	"browser-listener":    {"capture.browser"},
	"obs-alerts":          {"obs"},
	"guard-posture":       {"guard"},
	"guard-user-policy":   {"guard"},
	"guard-baseline-scan": {"guard"},
	"digest":              {"notify.email"},
	"process-observer":    {"processobs"},
	"node-intervention":   {"intervention"},

	"governance-sidecar":     {"org.client"},
	"budget-warn":            {"org.client"},
	"policy-state-reporter":  {"org.client"},
	"org-push":               {"org.client"},
	"policy-freshness":       {"org.client"},
	"activity-stamp":         {"org.client"},
	"org-policy-bundle-poll": {"org.client"},
	"policy-resource-poller": {"org.client"},
	"grant-renewal":          {"org.client"},
}

// gatedGroup is namedGroup behind the component gate: Go starts a loop only
// when daemonLoopComponents says one of its components is active, and says
// once on stderr which loops an inactive component kept from starting.
type gatedGroup struct {
	ng   namedGroup
	snap componentstate.Snapshot
	out  io.Writer

	mu      sync.Mutex
	skipped []string
}

func newGatedGroup(ng namedGroup, snap componentstate.Snapshot, out io.Writer) *gatedGroup {
	return &gatedGroup{ng: ng, snap: snap, out: out}
}

// Go runs fn as the named loop when its components allow it.
func (g *gatedGroup) Go(name string, fn func() error) {
	if !loopAllowed(g.snap, name) {
		g.mu.Lock()
		g.skipped = append(g.skipped, name)
		g.mu.Unlock()
		fmt.Fprintf(g.out, "components: %s not started (%s)\n", name, loopSkipReason(g.snap, daemonLoopComponents[name]))
		return
	}
	g.ng.Go(name, fn)
}

// loopSkipReason says why a gated loop did not start (D-514): a component
// compiled out of this binary is "not in this build" with the builds that
// include it; one that is merely inactive points at Settings > Components.
func loopSkipReason(snap componentstate.Snapshot, ids []string) string {
	var absent, inactive []string
	for _, id := range ids {
		if d, ok := snap.Decision(id); ok && d.State == components.StateAbsent {
			absent = append(absent, id)
		} else {
			inactive = append(inactive, id)
		}
	}
	var parts []string
	for _, id := range absent {
		parts = append(parts, id+" is not in this build; "+components.DistrosHaving(id))
	}
	if len(inactive) > 0 {
		parts = append(parts, strings.Join(inactive, " / ")+" inactive; "+components.SettingsHint())
	}
	return strings.Join(parts, "; ")
}

// loopAllowed is the per-loop decision. An unclassified name runs (fail
// open): the test, not the runtime, is what keeps the table complete.
func loopAllowed(snap componentstate.Snapshot, name string) bool {
	ids, ok := daemonLoopComponents[name]
	if !ok {
		return true
	}
	return snap.AnyActive(ids...)
}

// componentSnapshotFor resolves the component state a process should use
// for configPath. A config that does not load resolves the legacy
// everything-active snapshot, the same fail-open posture every other start
// gate takes on a missing config.
func componentSnapshotFor(configPath string) componentstate.Snapshot {
	cfg, err := config.Load(config.LoadOptions{GlobalPath: configPath})
	if err != nil {
		return componentstate.Legacy()
	}
	return componentSnapshotForConfig(cfg)
}

// reportComponentIssues prints the snapshot's selection problems once at
// start. An unconfigured snapshot has none, so a legacy install prints
// nothing. A distro build (phase P3) always prints the summary line, block
// or not: its base is its distro, not everything compiled, and the
// operator should see that at start.
func reportComponentIssues(w io.Writer, snap componentstate.Snapshot) {
	for _, is := range snap.Issues {
		fmt.Fprintf(w, "WARN components: %s\n", is)
	}
	if snap.Configured || snap.Base != componentstate.BaseAll {
		fmt.Fprintf(w, "components: %d of %d compiled components active (base %s; observer components status)\n",
			len(snap.ActiveIDs()), len(snap.Compiled), snap.Base)
	}
}

// inertOverlay is the start-time config overlay of an inactive component:
// the switches of the builders `observer start` itself runs, turned off so
// each builder takes its existing honest-disabled path (no stack, no
// listener, no loop). It is applied to the daemon's in-memory copy only and
// never written. Loops that reload their own config are gated by
// daemonLoopComponents instead.
var inertOverlay = []struct {
	component string
	apply     func(*config.Config)
}{
	{"terminals", func(c *config.Config) {
		c.Handoff.AllowDashboardLaunch = false
		c.Terminal.Attach.Enabled = false
	}},
	{"remote", func(c *config.Config) { c.Remote.Enabled = false }},
	{"export.otel", func(c *config.Config) { c.Exporter.OTel.Enabled = false }},
	{"obs", func(c *config.Config) { c.Observability.Enabled = false }},
	{"capture.browser", func(c *config.Config) { c.Browser.Enabled = false }},
	{"org.update", func(c *config.Config) { c.Update.Enabled = false }},
	{"integration.shell-wrap", func(c *config.Config) { c.ShellWrap.Enabled = false }},
}

// applyInertOverlay returns cfg with every inactive component's start-time
// switches off. A legacy snapshot returns cfg unchanged.
func applyInertOverlay(cfg config.Config, snap componentstate.Snapshot) config.Config {
	for _, row := range inertOverlay {
		if !snap.Active(row.component) {
			row.apply(&cfg)
		}
	}
	return cfg
}

// verbComponents maps every top-level verb an optional node component owns
// to that component: the catalog's Verbs (the one owner of verb ownership;
// the former override table was folded into the catalog, T-661) and every
// adapter's launcher subcommand from the integration registry.
func verbComponents() map[string]string {
	out := map[string]string{}
	for _, c := range components.ForBinary(components.BinaryNode) {
		if c.Class == components.ClassCore {
			continue
		}
		for _, v := range c.Verbs {
			out[v] = c.ID
		}
	}
	for _, tool := range integration.Tools() {
		c, ok := integration.For(tool)
		if !ok || c.Handoff.Launch == nil || c.Handoff.Launch.Subcommand == "" {
			continue
		}
		if _, known := components.Get(components.AdapterID(tool)); !known {
			continue
		}
		if _, taken := out[c.Handoff.Launch.Subcommand]; !taken {
			out[c.Handoff.Launch.Subcommand] = components.AdapterID(tool)
		}
	}
	return out
}

// gateComponentVerbs wraps every top-level verb of an optional component
// (and its subcommands) so it refuses with the component's state and the
// fix, exit code 3, when the component is not active (plan 7.3). The state
// is resolved when the verb runs, from the config the verb itself names
// with --config (else the default), so a bare CLI never trusts a
// daemon-written file.
func gateComponentVerbs(root *cobra.Command) {
	owners := verbComponents()
	for _, c := range root.Commands() {
		id, ok := owners[c.Name()]
		if !ok {
			continue
		}
		wrapVerbTree(c, id)
	}
}

func wrapVerbTree(c *cobra.Command, id string) {
	if c.RunE != nil || c.Run != nil {
		origE, orig := c.RunE, c.Run
		c.Run = nil
		c.RunE = func(cmd *cobra.Command, args []string) error {
			cfgPath := ""
			if f := cmd.Flags().Lookup("config"); f != nil {
				cfgPath = f.Value.String()
			}
			snap := componentSnapshotFor(cfgPath)
			switch {
			case snap.Active(id):
			case teardownVerb(cmd, id):
				// The component's own teardown step names this verb: it
				// undoes a write the component made, so it runs while the
				// component is inactive (the teardown preview tells the
				// operator to run it after removal).
				fmt.Fprintf(cmd.ErrOrStderr(), "components: %s is not active; running its teardown step\n", id)
			case offerActivation(cmd, snap, id, cfgPath):
				// T-734: turned on at the operator's yes; run the verb.
			default:
				fmt.Fprintln(cmd.ErrOrStderr(), snap.RefusalLine(id))
				// The line above is the whole message: no cobra error
				// banner, no usage.
				cmd.SilenceErrors, cmd.SilenceUsage = true, true
				if r := cmd.Root(); r != nil {
					r.SilenceErrors = true
				}
				return exitErr(componentstate.RefusalExitCode)
			}
			if origE != nil {
				return origE(cmd, args)
			}
			orig(cmd, args)
			return nil
		}
	}
	for _, sub := range c.Commands() {
		wrapVerbTree(sub, id)
	}
}

// teardownVerb reports whether cmd, as invoked, is the undo command one of
// component id's own teardown steps names (componentsvc.TeardownFor, the
// one owner of teardown steps; StepManual.Command). The command path must
// match exactly and every flag the step names must be set on the
// invocation, so `observer routing apply --tool claude-code --revert`
// passes while a plain `observer routing apply` (a write) stays refused.
// Without this, removing a component would refuse the very verb its
// teardown preview tells the operator to run (`observer mcp unwrap` for
// agentaccess.relay, T-681).
func teardownVerb(cmd *cobra.Command, id string) bool {
	got := strings.Fields(cmd.CommandPath())
	if len(got) < 2 {
		return false
	}
	got = got[1:] // drop the root's own name
	for _, st := range componentsvc.TeardownFor(id) {
		if st.Kind != componentsvc.StepManual || st.Command == "" {
			continue
		}
		path, flags := teardownCommandShape(st.Command)
		if strings.Join(path, " ") != strings.Join(got, " ") {
			continue
		}
		set := true
		for _, f := range flags {
			fl := cmd.Flags().Lookup(f)
			if fl == nil || !fl.Changed {
				set = false
				break
			}
		}
		if set {
			return true
		}
	}
	return false
}

// teardownCommandShape splits a teardown step's command line ("observer mcp
// unwrap", "observer routing apply --tool claude-code --revert") into its
// verb path after the binary name and the flag names it sets.
func teardownCommandShape(line string) (path, flags []string) {
	fields := strings.Fields(line)
	if len(fields) > 0 {
		fields = fields[1:] // the binary name
	}
	inFlags := false
	for _, f := range fields {
		if strings.HasPrefix(f, "--") {
			inFlags = true
			name, _, _ := strings.Cut(strings.TrimPrefix(f, "--"), "=")
			flags = append(flags, name)
			continue
		}
		if !inFlags {
			path = append(path, f)
		}
	}
	return path, flags
}

// gatedVerbNames lists the verbs gateComponentVerbs wraps, sorted (tests
// and `observer components status`).
func gatedVerbNames(root *cobra.Command) []string {
	owners := verbComponents()
	var out []string
	for _, c := range root.Commands() {
		if _, ok := owners[c.Name()]; ok {
			out = append(out, c.Name())
		}
	}
	sort.Strings(out)
	return out
}
