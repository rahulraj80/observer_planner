package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// structuralBase is an operator-written config.toml with comments.
const structuralBase = `# my settings - keep this header
[observer]
log_level = "warn" # inline note

[observer.watch]
# how often to poll
poll_interval_seconds = 5

[terminal]
enabled = true
`

// TestRenderPatchStructural pins T-524's writer: structural patches are
// applied key by key (comments kept, no defaults written out), and every
// shape the line editor cannot prove falls back to the re-serialize with a
// reason, never silently. One case per row.
func TestRenderPatchStructural(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		exists   bool
		edit     func(c *Config)
		wantMode PatchMode
		// wantIn must appear in the written text (surgical rows only).
		wantIn []string
		// wantOut must not appear in the written text (surgical rows only).
		wantOut []string
		// reasonHas must appear in the fallback reason (reserialize rows).
		reasonHas string
	}{
		{
			name: "scalar in an existing table", file: structuralBase, exists: true,
			edit:     func(c *Config) { c.Observer.Watch.PollIntervalSeconds = 9 },
			wantMode: PatchSurgical, wantIn: []string{"poll_interval_seconds = 9", "# how often to poll"},
		},
		{
			name: "list the file never set", file: structuralBase, exists: true,
			edit:     func(c *Config) { c.Observer.Watch.EnabledAdapters = []string{"codex"} },
			wantMode: PatchSurgical, wantIn: []string{`enabled_adapters = ["codex"]`},
		},
		{
			name: "two sections at once", file: structuralBase, exists: true,
			edit: func(c *Config) {
				c.Observer.LogLevel = "debug"
				c.Terminal.Enabled = false
			},
			wantMode: PatchSurgical, wantIn: []string{`log_level = "debug" # inline note`, "enabled = false"},
		},
		{
			name: "array of tables in the file", exists: true,
			file: structuralBase + "\n[[terminal.ssh.profiles]]\nname = \"box\"\nhost = \"box.example\"\n",
			edit: func(c *Config) {
				c.Terminal.SSH.Profiles = append(c.Terminal.SSH.Profiles, SSHProfileConfig{Name: "two", Host: "two.example"})
			},
			wantMode: PatchReserialize, reasonHas: "could not be edited in place",
		},
		{
			// D-407: a key under a nested table the file does not have gets
			// that table's own [header], never a dotted key under the
			// nearest ancestor (a later [observer.process] would redefine
			// the table and the file would stop loading).
			name: "keys under a nested table the file lacks", file: structuralBase, exists: true,
			edit: func(c *Config) {
				c.Observer.Process.Enabled = true
				c.Observer.Process.Backend = "poll"
			},
			wantMode: PatchSurgical,
			wantIn:   []string{"[observer.process]\nbackend = \"poll\"\nenabled = true", "[terminal]\nenabled = true"},
			wantOut:  []string{"process.enabled", "process.backend"},
		},
		{
			// A file that already writes the table as dotted keys keeps that
			// shape: the new key joins them.
			name: "a table the file writes as dotted keys stays dotted", exists: true,
			file:     strings.Replace(structuralBase, "log_level = \"warn\" # inline note\n", "log_level = \"warn\" # inline note\nprocess.backend = \"poll\"\n", 1),
			edit:     func(c *Config) { c.Observer.Process.Enabled = true },
			wantMode: PatchSurgical,
			wantIn:   []string{"process.backend = \"poll\"", "process.enabled = true"},
			wantOut:  []string{"[observer.process]"},
		},
		{
			// T-741 (D-551): a file that does not exist yet gets ONLY the
			// edited key, never every default (and never an [org_client]
			// block). Expectation changed from "re-serialized whole" by the
			// D-551 ruling: the old row pinned the defect.
			name: "file does not exist yet", file: "", exists: false,
			edit:     func(c *Config) { c.Observer.Watch.PollIntervalSeconds = 9 },
			wantMode: PatchSurgical,
			wantIn:   []string{"poll_interval_seconds = 9"},
			wantOut:  []string{"[org_client]", "log_level", "[terminal]"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prev := Default()
			if _, err := toml.Decode(c.file, &prev); err != nil {
				t.Fatal(err)
			}
			cfg := prev
			cfg.Terminal.SSH.Profiles = append([]SSHProfileConfig(nil), prev.Terminal.SSH.Profiles...)
			c.edit(&cfg)
			patches, err := StructuralPatches(prev, cfg)
			if err != nil || len(patches) == 0 {
				t.Fatalf("StructuralPatches = %v, %v", patches, err)
			}
			text, res, err := RenderPatch([]byte(c.file), c.exists, cfg, patches)
			if err != nil {
				t.Fatal(err)
			}
			if res.Mode != c.wantMode {
				t.Fatalf("mode = %s (reason %q), want %s", res.Mode, res.Reason, c.wantMode)
			}
			if c.wantMode == PatchReserialize {
				if !strings.Contains(res.Reason, c.reasonHas) || res.CommentsKept {
					t.Fatalf("fallback reason = %q (comments kept %v), want it to say %q", res.Reason, res.CommentsKept, c.reasonHas)
				}
				return
			}
			got := string(text)
			for _, keep := range []string{"# my settings - keep this header", "# inline note"} {
				if !strings.Contains(c.file, keep) {
					continue // a new file has no comments to keep
				}
				if !strings.Contains(got, keep) {
					t.Fatalf("lost %q:\n%s", keep, got)
				}
			}
			for _, w := range c.wantIn {
				if !strings.Contains(got, w) {
					t.Fatalf("missing %q:\n%s", w, got)
				}
			}
			for _, w := range c.wantOut {
				if strings.Contains(got, w) {
					t.Fatalf("unexpected %q:\n%s", w, got)
				}
			}
			// The written file loads to exactly the edited config.
			back := Default()
			if _, err := toml.Decode(got, &back); err != nil {
				t.Fatalf("written text does not decode: %v\n%s", err, got)
			}
			if again, _ := StructuralPatches(back, cfg); len(again) != 0 {
				t.Fatalf("written file differs from the edited config by %+v:\n%s", again, got)
			}
		})
	}
}

// TestStructuralPatchesDeleteOfADefaultFallsBack: deleting a key the file
// never set, whose default fills it back in on load, cannot be expressed by a
// key edit; the writer must notice and fall back rather than write a file
// that loads to something else.
func TestStructuralPatchesDeleteOfADefaultFallsBack(t *testing.T) {
	patches := []Patch{{Dotted: "observer.log_level", Path: []string{"observer", "log_level"}, Delete: true}}
	_, res, err := RenderPatch([]byte("[terminal]\nenabled = true\n"), true, Default(), patches)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != PatchReserialize || !strings.Contains(res.Reason, "does not load to the saved config") {
		t.Fatalf("mode = %s reason %q, want a reported fallback", res.Mode, res.Reason)
	}
}
