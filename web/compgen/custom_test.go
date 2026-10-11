package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// TestManifestSelectsTheCustomCompiledSet: -manifest (lane MP-CUST) drops
// the pages and packages of every component the custom build does not
// compile, exactly as -distro does for a prebuilt distro.
func TestManifestSelectsTheCustomCompiledSet(t *testing.T) {
	var omit string
	var cb components.CustomBuild
	for _, p := range pages {
		if p.Component == "core" {
			continue
		}
		c, ok := components.Get(p.Component)
		if !ok || !c.Distros[components.DistroDeveloper].Compiled() {
			continue
		}
		got, problems := components.ResolveManifest(components.Manifest{Schema: components.ManifestSchemaV1, Base: components.DistroDeveloper, Omit: []string{p.Component}})
		if len(problems) == 0 {
			omit, cb = p.Component, got
			break
		}
	}
	if omit == "" {
		t.Skip("no page component can leave the developer distro on its own")
	}
	path := filepath.Join(t.TempDir(), "c.json")
	//nolint:gosec // test fixture
	if err := os.WriteFile(path, components.CanonicalManifest(cb.Manifest, true), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { customSet, customLabel = nil, "" })
	if err := useManifest(path); err != nil {
		t.Fatal(err)
	}
	src, err := render("")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "Custom distro: "+cb.Distro) {
		t.Fatal("the registry does not name the custom distro")
	}
	pr := buildProof("")
	if pr.Distro != cb.Distro {
		t.Fatalf("proof distro %q", pr.Distro)
	}
	forbidden := strings.Join(pr.ForbiddenSources, ",")
	for _, p := range pages {
		if p.Component == omit && !strings.Contains(forbidden, "src/pages/"+p.Module) {
			t.Errorf("page %s of the omitted %s is not forbidden", p.Module, omit)
		}
		if p.Component == omit && strings.Contains(string(src), "@/pages/"+p.Module+"\"") {
			t.Errorf("page %s of the omitted %s is still imported", p.Module, omit)
		}
	}
}
