package main

import (
	"archive/tar"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/update"
)

// customFixtureManifest resolves a developer-based manifest omitting one
// omittable component, found from the catalog.
func customFixtureManifest(t *testing.T) components.CustomBuild {
	t.Helper()
	for _, id := range components.InDistro(components.DistroDeveloper) {
		cb, problems := components.ResolveManifest(components.Manifest{
			Schema: components.ManifestSchemaV1, Name: "fixture", Base: components.DistroDeveloper, Omit: []string{id},
		})
		if len(problems) == 0 {
			return cb
		}
	}
	t.Skip("no omittable developer component")
	return components.CustomBuild{}
}

// addCustomArchive writes and signs a custom linux archive into dir.
func addCustomArchive(t *testing.T, dir string, cb components.CustomBuild, priv []byte) string {
	t.Helper()
	name := update.DistroArtifactName(fixtureVersion, cb.Distro, "linux", "amd64")
	writeTarGz(t, filepath.Join(dir, name), []tarMember{
		{name: "observer", kind: tar.TypeReg, content: "the custom linux daemon"},
		{name: "superbased", kind: tar.TypeSymlink, link: "observer"},
	})
	writeSums(t, dir)
	if _, err := signArchive(filepath.Join(dir, name), priv); err != nil {
		t.Fatal(err)
	}
	return name
}

// TestReleaseIndexCustomEntryComesFromItsManifest: a custom archive is
// indexed with the catalog's resolution of the manifest that travels beside
// it (lane MP-CUST), and is refused without that manifest or with one that
// hashes to another id.
func TestReleaseIndexCustomEntryComesFromItsManifest(t *testing.T) {
	_, priv := testKey(t)
	dir := fixtureRelease(t, priv)
	cb := customFixtureManifest(t)
	name := addCustomArchive(t, dir, cb, priv)

	if _, _, _, err := buildReleaseIndex(BuildIndexOptions{Dir: dir, Version: fixtureVersion, Key: priv}); err == nil ||
		!strings.Contains(err.Error(), "needs its manifest") {
		t.Fatalf("a custom archive without its manifest: %v", err)
	}

	mpath := filepath.Join(dir, components.CustomManifestFileName(cb.Distro))
	other := cb.Manifest
	other.Omit = nil
	other.Add = nil
	other.Base = components.DistroFull
	//nolint:gosec // test fixture
	if err := os.WriteFile(mpath, components.CanonicalManifest(other, true), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := buildReleaseIndex(BuildIndexOptions{Dir: dir, Version: fixtureVersion, Key: priv}); err == nil {
		t.Fatal("a manifest that resolves to a prebuilt distro was accepted")
	}

	//nolint:gosec // test fixture
	if err := os.WriteFile(mpath, components.CanonicalManifest(cb.Manifest, true), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, ix, err := buildReleaseIndex(BuildIndexOptions{Dir: dir, Version: fixtureVersion, Key: priv})
	if err != nil {
		t.Fatalf("buildReleaseIndex: %v", err)
	}
	e, ok := ix.EntryByFilename(name)
	if !ok || e.Distro != cb.Distro || e.ComponentsDigest != cb.ComponentsDigest {
		t.Fatalf("custom entry %+v (ok %v), want distro %s digest %s", e, ok, cb.Distro, cb.ComponentsDigest)
	}
}
