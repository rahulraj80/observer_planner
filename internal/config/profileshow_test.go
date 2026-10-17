package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestEncodeResolvedCompression pins the one renderer `observer profile show`
// and the dashboard's profile view share: TOML under [compression], with the
// config file's own key names, that decodes back to the same parameters.
func TestEncodeResolvedCompression(t *testing.T) {
	master := Default().Compression
	resolved, _, err := ProfileStore{}.ResolveCompression(master, "codex-safe")
	if err != nil {
		t.Fatal(err)
	}
	text, err := EncodeResolvedCompression(resolved)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[compression.conversation]", "target_ratio", "[compression.shell]"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "TargetRatio") {
		t.Errorf("output uses Go field names, want TOML keys:\n%s", text)
	}
	var back struct {
		Compression CompressionConfig `toml:"compression"`
	}
	if _, err := toml.Decode(text, &back); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(back.Compression, resolved) {
		t.Errorf("round trip changed the parameters")
	}
}
