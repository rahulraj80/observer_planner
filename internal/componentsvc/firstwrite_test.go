package componentsvc

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestFirstWriteOnAFreshInstallIsMinimal pins T-741 (D-551): the first
// [components] write on a machine with no config.toml writes a file holding
// the [components] block and nothing else. Before the fix the write
// re-serialized every default (a 665-line file with no comments) including a
// disabled, empty [org_client] block, so a later `observer enroll` found an
// [org_client] block it had not written and left pushing off.
func TestFirstWriteOnAFreshInstallIsMinimal(t *testing.T) {
	s, path, _, _ := newSvc(t, "")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s must not exist yet (err %v)", path, err)
	}
	ch := Change{Remove: []string{"codeintel"}}
	p, err := s.Preview(context.Background(), ch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(context.Background(), ch, ApplyOptions{ExpectFingerprint: p.Fingerprint, SkipTeardown: true}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	var tables map[string]any
	if _, err := toml.Decode(text, &tables); err != nil {
		t.Fatalf("written file does not decode: %v\n%s", err, text)
	}
	if len(tables) != 1 || tables["components"] == nil {
		t.Fatalf("a first [components] write must hold only [components], got tables %v:\n%s", keysOf(tables), text)
	}
	for _, never := range []string{"[org_client]", "org_server_url", "enabled = false"} {
		if strings.Contains(text, never) {
			t.Fatalf("a components edit wrote %q:\n%s", never, text)
		}
	}
	if !strings.Contains(text, `"codeintel"`) {
		t.Fatalf("the edit itself is missing:\n%s", text)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
