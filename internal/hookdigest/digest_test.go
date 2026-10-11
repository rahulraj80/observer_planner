package hookdigest

import (
	"errors"
	"testing"
)

// TestHooksSubtreeTOML: a TOML [[hooks]] list digests like the same list in
// JSON (the digest covers the hooks value, not the format), the rest of the
// file never moves the digest, and an unparseable file is refused without
// detail (T-378, Kimi Code's config.toml).
func TestHooksSubtreeTOML(t *testing.T) {
	const tomlHooks = "api_key = \"sk-test-not-real\"\n\n[[hooks]]\nevent = \"PreToolUse\"\ncommand = \"observer hook kimi-code PreToolUse\"\ntimeout = 30\n"
	const jsonHooks = `{"hooks":[{"event":"PreToolUse","command":"observer hook kimi-code PreToolUse","timeout":30}]}`
	got, err := HooksSubtree("config.toml", []byte(tomlHooks))
	if err != nil {
		t.Fatal(err)
	}
	want, err := HooksSubtree("x.json", []byte(jsonHooks))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("toml digest %s != json digest %s", got, want)
	}
	other, err := HooksSubtree("config.toml", []byte("api_key = \"another\"\n\n[[hooks]]\nevent = \"PreToolUse\"\ncommand = \"observer hook kimi-code PreToolUse\"\ntimeout = 30\n"))
	if err != nil || other != got {
		t.Errorf("a non-hooks key moved the digest: %s %v", other, err)
	}
	empty, err := HooksSubtree("config.toml", nil)
	if err != nil {
		t.Fatal(err)
	}
	null, _ := HooksSubtree("x.json", []byte(`{}`))
	if empty != null {
		t.Errorf("an empty TOML file must digest as an absent hooks value")
	}
	if _, err := HooksSubtree("config.toml", []byte("[[hooks]\n")); !errors.Is(err, ErrUnparseable) {
		t.Errorf("unparseable TOML: err = %v", err)
	}
	if _, err := HooksSubtree("config.ini", []byte("x")); !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("unknown extension: err = %v", err)
	}
}
