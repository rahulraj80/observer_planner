//go:build !sbo_omit_agentaccess_relay

package mcpregister

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/mcp/locate"
	"github.com/marmutapp/superbased-observer/internal/mcprelay/project"
)

// T-397: the relay writer edits only the relay-owned entries of a
// credential-bearing client config (D-268 / D-270). Every other byte stays
// (spacing, key order, comments, a sibling's env block), the owned digests
// are never a digest of the whole file, and an entry it cannot edit in
// place is refused without quoting the file.

const relayFakeKey = "sk-test-not-real-relay"

func TestRelayWriterEditsOnlyRelayOwnedEntriesJSON(t *testing.T) {
	ctx := context.Background()
	r, home := newRelayRegistrar(t)
	path := filepath.Join(home, ".cursor", "mcp.json")
	writeConfig(t, path, priorJSON)
	c := project.Client{Tool: "cursor", ConfigPath: path, Format: string(locate.FormatMCPServersJSON), Verified: true}
	st, err := r.Stage(ctx, c, desiredAll)
	if err != nil || !st.Changed {
		t.Fatalf("stage %+v %v", st, err)
	}
	kept := []string{
		`    "zzzTop":   {"shown": true},`,
		`        "other-remote": {"url": "https://x.example/mcp", "type": "sse", "headers": {"X-A": "1"}, "env": {"K": "v"}},`,
		`"env": {"GITHUB_TOKEN": "ghp_SECRETVALUE"}, "cwd": "/repo", "timeout": 30}`,
		`    "observer": {"command": "/usr/local/bin/observer", "args": ["serve"]}`,
	}
	for _, k := range kept {
		if !strings.Contains(string(st.After), k) {
			t.Errorf("the stage re-rendered bytes it does not own; lost %q in:\n%s", k, st.After)
		}
	}
	whole := sha256.Sum256(st.After)
	for key, d := range st.Owned {
		if d == hex.EncodeToString(whole[:]) || d == "" {
			t.Fatalf("owned digest of %s is a whole-file digest", key)
		}
	}
	if want := project.OwnedStdioDigest("gh", observerBin, project.WrapArgs("cursor", "gh", wrapSpec)); st.Owned["gh"] != want {
		t.Fatalf("gh owned digest = %q, want the wrapper launch digest %q", st.Owned["gh"], want)
	}
	if _, ok := st.Owned[relayName]; !ok {
		t.Fatalf("no owned digest for the relay remote entry: %v", st.Owned)
	}
	if err := r.Commit(ctx, c, st); err != nil {
		t.Fatal(err)
	}
	owned, err := r.Owned(ctx, c, rowsFromStage(c, st))
	if err != nil || owned["gh"] != st.Owned["gh"] || owned[relayName] != st.Owned[relayName] {
		t.Fatalf("Owned after commit = %v %v, want the staged digests %v", owned, err, st.Owned)
	}
	if err := r.Revert(ctx, c, rowsFromStage(c, st)); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	for _, k := range kept {
		if !strings.Contains(string(after), k) {
			t.Errorf("the reversal re-rendered bytes it does not own; lost %q in:\n%s", k, after)
		}
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatal("a backup copy was written")
	}
}

func TestRelayWriterEditsOnlyRelayOwnedEntriesTOML(t *testing.T) {
	ctx := context.Background()
	r, home := newRelayRegistrar(t)
	path := filepath.Join(home, ".codex", "config.toml")
	writeConfig(t, path, priorCodexTOML)
	c := project.Client{Tool: "codex", ConfigPath: path, Format: string(locate.FormatCodexTOML), Verified: true}
	st, err := r.Stage(ctx, c, desiredAll)
	if err != nil || !st.Changed {
		t.Fatalf("stage %+v %v", st, err)
	}
	for _, k := range []string{"# codex config\nmodel = \"gpt-5\"\n", "[sandbox]\nmode = \"workspace-write\"\n", "[mcp_servers.gh.env]\nGITHUB_TOKEN = \"ghp_SECRETVALUE\"\n", "cwd = \"/repo\"\nstartup_timeout_sec = 20\n"} {
		if !strings.Contains(string(st.After), k) {
			t.Errorf("the TOML stage re-rendered bytes it does not own; lost %q in:\n%s", k, st.After)
		}
	}
	if err := r.Commit(ctx, c, st); err != nil {
		t.Fatal(err)
	}
	if err := r.Revert(ctx, c, rowsFromStage(c, st)); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !strings.HasPrefix(string(got), "# codex config\n") || !strings.Contains(string(got), "[mcp_servers.gh.env]\nGITHUB_TOKEN = \"ghp_SECRETVALUE\"\n") {
		t.Fatalf("the reversal lost bytes it does not own:\n%s", got)
	}
}

func TestRelayWriterRefusesInlineEntriesWithoutQuotingTheFile(t *testing.T) {
	ctx := context.Background()
	r, home := newRelayRegistrar(t)
	path := filepath.Join(home, ".codex", "config.toml")
	body := "# keep\nmcp_servers = { gh = { command = \"npx\", env = { TOKEN = \"" + relayFakeKey + "\" } } }\n"
	writeConfig(t, path, body)
	c := project.Client{Tool: "codex", ConfigPath: path, Format: string(locate.FormatCodexTOML), Verified: true}
	_, err := r.Stage(ctx, c, desiredAll)
	if err == nil {
		t.Fatal("an inline mcp_servers table must be refused, not re-serialized")
	}
	if strings.Contains(err.Error(), relayFakeKey) {
		t.Fatalf("the refusal quotes the file: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != body {
		t.Fatal("a refused stage touched the file")
	}
	broken := filepath.Join(home, ".cursor", "mcp.json")
	writeConfig(t, broken, `{"mcpServers": {"gh": {"command": "npx", "env": {"T": "`+relayFakeKey+`"}}}`)
	cb := project.Client{Tool: "cursor", ConfigPath: broken, Format: string(locate.FormatMCPServersJSON), Verified: true}
	if _, err := r.Stage(ctx, cb, desiredAll); err == nil || strings.Contains(err.Error(), relayFakeKey) {
		t.Fatalf("a config that does not parse must be refused without quoting it: %v", err)
	}
}
