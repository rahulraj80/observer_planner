//go:build !sbo_omit_terminals_sandbox_sandhold && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/sandbox"
	"github.com/marmutapp/superbased-observer/internal/sandboxbackend"
	"github.com/marmutapp/superbased-observer/internal/sandboxremote/sandhold"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// TestSandboxStatusListsSandHoldTiers (demo round 2026-11-12): `observer
// sandbox status` listed the sandhold backend as CLASS process, the floor
// Observer counts the row as, for a remote node offering four tiers. The
// row now lists the tiers the connected node offers, each with its class,
// networks and targets, active or not; a node not connected says the row
// is by tier and how to see them. Every other row keeps its one class.
func TestSandboxStatusListsSandHoldTiers(t *testing.T) {
	_, r := sandholdTestRow(t, nil)
	render := func() (dashapi.SandboxBackendAvail, string) {
		t.Helper()
		cfg := config.Default().Terminal.Sandbox
		cfg.Enabled = true
		cfg.Backends = []string{sandbox.BackendBwrap, sandboxbackend.BackendSandHold}
		r.cfg = cfg
		r.buildSandboxDrivers(config.Default())
		backends := sandboxBackendListWith(cfg, sandbox.Availability{}, nil, vocab.TierNone, r.driverProbes(t.Context()))
		var buf bytes.Buffer
		if err := renderSandboxStatus(&buf, sandboxStatusReport{Enabled: true, Egress: "none", Host: "linux", Backends: backends}); err != nil {
			t.Fatal(err)
		}
		for _, b := range backends {
			if b.ID == sandboxbackend.BackendSandHold {
				return b, buf.String()
			}
		}
		t.Fatal("no sandhold row")
		return dashapi.SandboxBackendAvail{}, ""
	}
	tableRow := func(out, id string) string {
		for _, l := range strings.Split(out, "\n") {
			if f := strings.Fields(l); len(f) > 1 && f[0] == id {
				return l
			}
		}
		return ""
	}

	b, out := render()
	if len(b.Tiers) != 0 || !strings.HasPrefix(strings.Fields(tableRow(out, "sandhold"))[1], "by") {
		t.Fatalf("not connected: tiers %v, row %q", b.Tiers, tableRow(out, "sandhold"))
	}
	if !strings.Contains(out, "sandhold: isolation by tier: the node gives each run the tier it asks for; connect to a node") {
		t.Fatalf("not connected: no tier line:\n%s", out)
	}

	if err := sandholdSaveConnection(r.observerDir, sandhold.Connection{URL: "https://node.example.com", User: "alice", Offers: []sandhold.Offer{
		{Tier: "shared", Class: vocab.ClassProcess, Networks: []string{"open"}, Targets: []string{"jail"}},
		{Tier: "shielded", Class: vocab.ClassContainer, Networks: []string{"none", "limited", "open"}, Targets: []string{"runc"}, Default: true},
		{Tier: "sealed", Class: vocab.ClassUserspaceKernel, Networks: []string{"none", "limited", "open"}, Targets: []string{"gvisor"}},
		{Tier: "vault", Class: vocab.ClassHWVM, Networks: []string{"none", "limited", "open"}, Targets: []string{"kata"}},
	}}); err != nil {
		t.Fatal(err)
	}
	b, out = render()
	if len(b.Tiers) != 4 || b.Tiers[1].Tier != "shielded" || b.Tiers[1].Title != "Shielded" || b.Tiers[1].Class != "container" || !b.Tiers[1].Default {
		t.Fatalf("connected: tiers %+v", b.Tiers)
	}
	row := tableRow(out, "sandhold")
	if f := strings.Fields(row); f[1] != "shared,shielded,sealed,vault" {
		t.Fatalf("connected: CLASS cell %q in %q", f[1], row)
	}
	for _, want := range []string{
		"  Shared: class process; network open; runs on jail",
		"  Shielded: class container; network none, limited, open; runs on runc (default)",
		"  Sealed: class userspace_kernel; network none, limited, open; runs on gvisor",
		"  Vault: class hw_vm; network none, limited, open; runs on kata",
		"Observer counts the row as process for isolation floors",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("connected: missing %q in:\n%s", want, out)
		}
	}
	if f := strings.Fields(tableRow(out, "bwrap")); len(f) < 2 || f[1] != string(vocab.ClassProcess) {
		t.Errorf("bwrap row %q", tableRow(out, "bwrap"))
	}

	// The component not selected: the row still lists what the node
	// offers, from the same record.
	reg := sandboxDriverRegs[sandboxbackend.BackendSandHold]
	if reg.tiers == nil || len(reg.tiers(r)) != 4 {
		t.Fatal("the inactive row does not list the node's tiers")
	}
}
