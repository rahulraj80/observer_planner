//go:build !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
	"github.com/marmutapp/superbased-observer/internal/termsvc"
	"github.com/marmutapp/superbased-observer/internal/workspace"
)

// TestHostedSourceDelivery (lane G4, design 4.1 and 4.3): one case per
// source on a vendor with Deliveries, the unchanged rule of a vendor with
// none, the empty host list refused before any validation, and the
// remote-URL attack shapes refused.
func TestHostedSourceDelivery(t *testing.T) {
	org, _ := sandboxremote.VendorFor(sandboxremote.VendorOpenShellOrg)
	e2b, _ := sandboxremote.VendorFor(sandboxremote.VendorE2B)
	cfg := config.TerminalSandboxConfig{AllowRemoteClone: true, RemoteAllowedHosts: []string{"git.example.com"}}
	cases := []struct {
		name     string
		v        sandboxremote.Vendor
		src, url string
		cfg      config.TerminalSandboxConfig
		want     workspace.Delivery
		refusal  string
	}{
		{"file-API vendor, default", e2b, "", "", cfg, workspace.DeliverySync, ""},
		{"file-API vendor, live", e2b, "live", "", cfg, workspace.DeliverySync, ""},
		{"file-API vendor, clone-local refused", e2b, "clone-local", "", cfg, "", "source_unsupported"},
		{"org row, default is clone-local", org, "", "", cfg, workspace.DeliverySync, ""},
		{"org row, clone-local", org, "clone-local", "", cfg, workspace.DeliverySync, ""},
		{"org row, live refused with the next step", org, "live", "", cfg, "", "clone-local"},
		{"org row, worktree refused", org, "worktree", "", cfg, "", "source_unsupported"},
		{"org row, clone-remote", org, "clone-remote", "https://git.example.com/acme/app.git", cfg, workspace.DeliveryRemoteClone, ""},
		{"clone-remote without allow_remote_clone", org, "clone-remote", "https://git.example.com/acme/app.git", config.TerminalSandboxConfig{RemoteAllowedHosts: []string{"git.example.com"}}, "", "allow_remote_clone"},
		{"clone-remote with an empty host list", org, "clone-remote", "https://git.example.com/acme/app.git", config.TerminalSandboxConfig{AllowRemoteClone: true}, "", "remote_clone_hosts_missing"},
		{"another host", org, "clone-remote", "https://evil.example.net/x.git", cfg, "", "clone_url_invalid"},
		{"ext transport", org, "clone-remote", "ext::sh -c touch% /tmp/pwned", cfg, "", "clone_url_invalid"},
		{"file url", org, "clone-remote", "file:///etc", cfg, "", "clone_url_invalid"},
		{"leading dash", org, "clone-remote", "--upload-pack=touch /tmp/x", cfg, "", "clone_url_invalid"},
		{"upload-pack embedded", org, "clone-remote", "https://git.example.com/a.git --upload-pack=x", cfg, "", "clone_url_invalid"},
	}
	for _, tc := range cases {
		got, err := hostedSourceDelivery(tc.v, termsvc.PrepareRequest{WorkspaceSource: tc.src, WorkspaceRemote: tc.url}, tc.cfg)
		if tc.refusal != "" {
			if err == nil || !strings.Contains(err.Error(), tc.refusal) {
				t.Errorf("%s: err %v, want %q", tc.name, err, tc.refusal)
			}
			continue
		}
		if err != nil || got.Delivery != tc.want {
			t.Errorf("%s: %+v %v", tc.name, got, err)
		}
		if got.Delivery == workspace.DeliveryRemoteClone && (got.CloneURL != tc.url || got.uploads()) {
			t.Errorf("%s: a remote clone uploads or lost its URL: %+v", tc.name, got)
		}
	}
}
