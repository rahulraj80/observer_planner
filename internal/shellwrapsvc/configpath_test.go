package shellwrapsvc

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestShimCarriesNonDefaultConfig pins P3-09 end to end: a shim enabled
// under a non-default config runs `observer <verb> --config <that file>`, so
// the wrapped launch uses that config's proxy port, DB, allowed tools and
// budget state; a shim enabled under the default config stays unchanged
// (no --config). Status reads the shim back as in sync either way.
func TestShimCarriesNonDefaultConfig(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	cases := []struct {
		name    string
		config  func(home string) string
		wantArg func(home string) string
	}{
		{
			"custom config", func(h string) string { return filepath.Join(h, "custom", "observer.toml") },
			func(h string) string { return " --config " + filepath.Join(h, "custom", "observer.toml") },
		},
		{
			"default config spelled out", func(h string) string { return filepath.Join(h, ".observer", "config.toml") },
			func(string) string { return "" },
		},
		{"no config flag", func(string) string { return "" }, func(string) string { return "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.svc.ConfigPath = tc.config(f.home)
			c := routedCLI(t)
			bashrc := filepath.Join(f.home, ".bashrc")
			writeFile(t, bashrc, "", 0o644)
			f.apply([]string{c.ID})
			script := ". '" + bashrc + "'; " + c.Commands[0] + " --flag x"
			cmd := exec.Command("sh", "-c", script) //nolint:gosec // test
			cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + f.home}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("run: %v %s", err, out)
			}
			want := c.Wrapped + tc.wantArg(f.home) + " -- --flag x\n"
			if got := read(t, f.log); got != want {
				t.Fatalf("observer got %q, want %q", got, want)
			}
			st, err := f.svc.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !st.InSync {
				t.Fatalf("status not in sync after apply: %+v", st)
			}
		})
	}
}
