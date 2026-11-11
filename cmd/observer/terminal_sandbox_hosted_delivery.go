//go:build !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"fmt"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
	"github.com/marmutapp/superbased-observer/internal/sandboxselect"
	"github.com/marmutapp/superbased-observer/internal/termsvc"
	"github.com/marmutapp/superbased-observer/internal/workspace"
)

// terminal_sandbox_hosted_delivery.go resolves a hosted launch's workspace
// source through the vendor row's Deliveries capability and
// workspace.DeliveryFor (lane G4, g1-design.md section 4.1). A vendor with
// no Deliveries is what the three file-API vendors are today: the live tree
// uploaded (sync), any other source refused, unchanged. The branch is on the
// delivery value, never on a vendor id.

// hostedDelivery is a launch's resolved workspace delivery.
type hostedDelivery struct {
	// Source and Delivery are the resolved values ("" source on a vendor
	// with no Deliveries: the earlier live-upload rule).
	Source   workspace.Source
	Delivery workspace.Delivery
	// CloneURL is the validated URL of a remote_clone delivery, cloned in
	// the box (never on this machine).
	CloneURL string
}

// uploads reports whether the delivery uploads this machine's tree.
func (h hostedDelivery) uploads() bool { return h.Delivery != workspace.DeliveryRemoteClone }

// hostedSourceDelivery resolves req's workspace source for vendor v under
// the effective sandbox config cfg.
func hostedSourceDelivery(v sandboxremote.Vendor, req termsvc.PrepareRequest, cfg config.TerminalSandboxConfig) (hostedDelivery, error) {
	src := strings.TrimSpace(req.WorkspaceSource)
	if len(v.Deliveries) == 0 {
		if src != "" && src != "live" {
			return hostedDelivery{}, fmt.Errorf("terminal_sandbox: %s: a hosted box receives the live workspace as an upload; the %s source is not offered", sandboxselect.VerdictSourceUnsupported, src)
		}
		return hostedDelivery{Delivery: workspace.DeliverySync}, nil
	}
	if src == "" {
		// The default source of a row with its own deliveries is a local
		// clone (the picker preselects it).
		src = string(workspace.SourceCloneLocal)
	}
	parsed, err := workspace.ParseSource(src)
	if err != nil {
		return hostedDelivery{}, err
	}
	modes := make([]workspace.Delivery, 0, len(v.Deliveries))
	for _, d := range v.Deliveries {
		modes = append(modes, workspace.Delivery(d))
	}
	d, err := workspace.DeliveryFor(parsed, modes)
	if err != nil {
		next := ""
		if parsed == workspace.SourceLive {
			next = "; use the clone-local source, which uploads your tracked files"
		}
		return hostedDelivery{}, fmt.Errorf("terminal_sandbox: %s: %w%s", sandboxselect.VerdictSourceUnsupported, err, next)
	}
	out := hostedDelivery{Source: parsed, Delivery: d}
	if d != workspace.DeliveryRemoteClone {
		return out, nil
	}
	// A box-side clone (design 4.3): the existing remote-clone gate, a
	// non-empty effective host list (never the "empty allows every host"
	// branch), and the URL checks; the org's mint checks them again.
	if !cfg.AllowRemoteClone {
		return hostedDelivery{}, fmt.Errorf("terminal_sandbox: %s: the clone-remote source needs allow_remote_clone", sandboxselect.VerdictSourceUnsupported)
	}
	if len(cfg.RemoteAllowedHosts) == 0 {
		return hostedDelivery{}, fmt.Errorf("terminal_sandbox: remote_clone_hosts_missing: on your organization's compute the organization must name its git hosts (remote_allowed_hosts)")
	}
	if err := workspace.ValidateRemoteURL(req.WorkspaceRemote, cfg.RemoteAllowedHosts); err != nil {
		return hostedDelivery{}, fmt.Errorf("terminal_sandbox: clone_url_invalid: %w", err)
	}
	out.CloneURL = req.WorkspaceRemote
	return out, nil
}
