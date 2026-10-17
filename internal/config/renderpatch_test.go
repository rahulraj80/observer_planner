package config

import (
	"os"
	"path/filepath"
	"testing"
)

// RenderPatch is PatchFile's dry-run half: the bytes it renders are the
// bytes PatchFile writes, for both strategies, and a no-op renders Wrote=false.
func TestRenderPatchMatchesPatchFile(t *testing.T) {
	const body = "# keep\n[observer]\nlog_level = \"warn\"\n"
	cases := []struct {
		name    string
		patches []Patch
		mode    PatchMode
		wrote   bool
	}{
		{"surgical", []Patch{{Dotted: "observer.log_level", RHS: `"debug"`, Scalar: true}}, PatchSurgical, true},
		{"surgical no-op", []Patch{{Dotted: "observer.log_level", RHS: `"warn"`, Scalar: true}}, PatchSurgical, false},
		{"reserialize", []Patch{{Dotted: "observer.watch.enabled_adapters"}}, PatchReserialize, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(LoadOptions{GlobalPath: path})
			if err != nil {
				t.Fatal(err)
			}
			cfg.Observer.LogLevel = "debug"
			text, res, err := RenderPatch([]byte(body), true, cfg, tc.patches)
			if err != nil {
				t.Fatal(err)
			}
			if res.Mode != tc.mode || res.Wrote != tc.wrote {
				t.Fatalf("RenderPatch: %+v", res)
			}
			after, _ := os.ReadFile(path)
			if string(after) != body {
				t.Fatal("RenderPatch must not write")
			}
			pres, err := PatchFile(path, cfg, tc.patches)
			if err != nil {
				t.Fatal(err)
			}
			written, _ := os.ReadFile(path)
			if pres.Wrote != tc.wrote || (tc.wrote && string(written) != string(text)) {
				t.Fatalf("PatchFile wrote %q (%+v), RenderPatch rendered %q", written, pres, text)
			}
		})
	}
}
