package sandhold

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// contract_box_test.go replays the customer contract's sandbox and exec
// answers against the launch path's wire client (Box): every call it makes
// that has a vector (sandbox-create, sandbox-create-refused, sandbox-get,
// sandbox-list, sandbox-delete, sandbox-missing, exec-stream).

// contractBox is a Box over contractNode.
func contractBox(t *testing.T, vs ...vector) *Box {
	t.Helper()
	c := contractNode(t, vs...)
	c.Key = "k"
	c.HTTP = streamHTTPClient()
	return &Box{api: c}
}

// TestContractBoxCreate: a create sends the contract's fields, reads the id,
// state, isolation, network and target, and says them in the node's words.
func TestContractBoxCreate(t *testing.T) {
	v := loadVector(t, "sandbox-create")
	b := contractBox(t, v)
	got, err := b.Create(context.Background(), sandboxremote.BoxSpec{
		RunID: "run1", Labels: map[string]string{sandboxremote.LabelKey: "run1"},
		Template: "py-tests", TimeoutSeconds: 600, Tier: vocab.TierInternet, Isolation: vocab.ClassContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != contractID || !got.Running || got.Extra["isolation"] != "shielded" || got.Extra["network"] != "limited" || got.Extra["runs_on"] != "runc" {
		t.Errorf("the create was not read as the contract gives it: %+v", got)
	}
	if got.Described != "Shielded (a hardened container on the host's kernel), network limited, runs on runc" {
		t.Errorf("described as %q", got.Described)
	}
}

// TestContractBoxCreateGotLess: a box the node says got less than asked
// (an isolation below the class, a network more open than the tier) is
// refused and deleted, never used.
func TestContractBoxCreateGotLess(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec sandboxremote.BoxSpec
		want string
	}{
		{"isolation", sandboxremote.BoxSpec{Template: "py-tests", TimeoutSeconds: 600, Tier: vocab.TierInternet, Isolation: vocab.ClassHWVM}, "Shielded isolation, below the Vault asked for"},
		{"network", sandboxremote.BoxSpec{Template: "py-tests", TimeoutSeconds: 600, Tier: vocab.TierNone}, "network limited, more open than the none tier allows"},
		{"proxy_only", sandboxremote.BoxSpec{Template: "py-tests", TimeoutSeconds: 600, Tier: vocab.TierProxyOnly}, "more open than the proxy_only tier allows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := contractBox(t, loadVector(t, "sandbox-create"), loadVector(t, "sandbox-delete"))
			_, err := b.Create(context.Background(), tc.spec)
			if !errors.Is(err, sandboxremote.ErrRefused) || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "the sandbox was deleted") {
				t.Fatalf("got %v, want a refusal naming %q and the delete", err, tc.want)
			}
		})
	}
}

// TestContractBoxCreateRefused: the node's refusal reaches the person in the
// node's own words, with its fix, and is never read as a lost create.
func TestContractBoxCreateRefused(t *testing.T) {
	v := loadVector(t, "sandbox-create-refused")
	var words struct {
		Error string `json:"error"`
		Fix   string `json:"fix"`
	}
	_ = json.Unmarshal(v.Response, &words)
	_, err := contractNode(t, v).CreateSandbox(context.Background(), CreateRequest{Isolation: "moon"})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != v.Status || ae.Msg != words.Error || ae.Fix != words.Fix {
		t.Fatalf("got %v, want the node's own words %q and fix %q", err, words.Error, words.Fix)
	}
	_, err = contractBox(t, v).Create(context.Background(), sandboxremote.BoxSpec{Tier: vocab.TierNone, Isolation: vocab.ClassContainer, TimeoutSeconds: 60})
	if err == nil || errors.Is(err, sandboxremote.ErrCreateUnknown) || !strings.Contains(err.Error(), words.Error) || !strings.Contains(err.Error(), words.Fix) {
		t.Errorf("the launch would say %v, want the node's reason and fix", err)
	}
}

func TestContractBoxGetListDelete(t *testing.T) {
	b := contractBox(t, loadVector(t, "sandbox-get"))
	got, err := b.Get(context.Background(), contractID)
	if err != nil || got.ID != contractID || !got.Running || got.Extra["network"] != "limited" {
		t.Errorf("get: %+v %v", got, err)
	}
	_, err = contractBox(t, loadVector(t, "sandbox-missing")).Get(context.Background(), contractID)
	if !errors.Is(err, sandboxremote.ErrNotFound) || !strings.Contains(err.Error(), "sandbox not found") {
		t.Errorf("missing: %v, want ErrNotFound in the node's words", err)
	}
	list, err := contractNode(t, loadVector(t, "sandbox-list")).ListSandboxes(context.Background())
	if err != nil || len(list) != 1 || list[0].ID != contractID || list[0].Isolation != "shielded" || list[0].Where() != "runc" {
		t.Errorf("list: %+v %v", list, err)
	}
	if err := contractBox(t, loadVector(t, "sandbox-delete")).Delete(context.Background(), contractID); err != nil {
		t.Errorf("delete (%d, no body): %v", loadVector(t, "sandbox-delete").Status, err)
	}
	// The list carries no metadata: a box is never "found" by its label.
	if _, err := contractBox(t).ListByLabel(context.Background(), sandboxremote.LabelKey, "run1"); !errors.Is(err, sandboxremote.ErrNotSupported) {
		t.Errorf("list by label: %v, want ErrNotSupported", err)
	}
}

// TestContractBoxExecStream: a command with no stdin runs on the streamed
// exec; its output arrives as the node sent it, then its exit code.
func TestContractBoxExecStream(t *testing.T) {
	b := contractBox(t, loadVector(t, "exec-stream"))
	p, err := b.Exec(context.Background(), contractID, sandboxremote.ExecSpec{Argv: []string{"echo", "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	out, _ := io.ReadAll(p)
	code, err := p.Wait(context.Background())
	if string(out) != "ok\n" || code != 0 || err != nil {
		t.Errorf("got %q exit %d %v, want the contract's output and exit 0", out, code, err)
	}
}
