package main

import (
	"archive/tar"
	"crypto/ed25519"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/update"
)

// TestReleaseIndexCoversEveryDistroAndVerifies: the index lists the full and
// distro archives (and nothing else), binds each to its distro's catalog
// digest, verifies with the signing key and refuses any other key. The key
// is a throwaway generated per run.
func TestReleaseIndexCoversEveryDistroAndVerifies(t *testing.T) {
	pub, priv := testKey(t)
	dir := fixtureRelease(t, priv)
	minimal := update.DistroArtifactName(fixtureVersion, "minimal", "linux", "amd64")
	writeTarGz(t, filepath.Join(dir, minimal), []tarMember{
		{name: "observer", kind: tar.TypeReg, content: "the minimal linux daemon"},
		{name: "superbased", kind: tar.TypeSymlink, link: "observer"},
	})
	writeSums(t, dir)
	if _, err := signArchive(filepath.Join(dir, minimal), priv); err != nil {
		t.Fatal(err)
	}

	body, sig, ix, err := buildReleaseIndex(BuildIndexOptions{Dir: dir, Version: fixtureVersion, Key: priv})
	if err != nil {
		t.Fatalf("buildReleaseIndex: %v", err)
	}
	if len(ix.Artifacts) != 3 {
		t.Fatalf("artifacts = %d, want full linux, full win32 and minimal linux (no org archive)", len(ix.Artifacts))
	}
	got := map[string]update.ReleaseIndexEntry{}
	for _, a := range ix.Artifacts {
		got[a.Filename] = a
		if strings.HasPrefix(a.Filename, "observer-org-") {
			t.Fatalf("the org archive is in the agent index: %s", a.Filename)
		}
	}
	m := got[minimal]
	if m.Distro != "minimal" || m.ComponentsDigest != components.Digest(components.InDistro(components.DistroMinimal)) {
		t.Fatalf("minimal entry: %+v", m)
	}
	full := got["observer-"+fixtureVersion+"-linux-x64.tar.gz"]
	if full.Distro != "full" || full.ComponentsDigest != components.Digest(components.InDistro(components.DistroFull)) {
		t.Fatalf("full entry: %+v", full)
	}
	if _, err := update.VerifyReleaseIndex(body, sig, []ed25519.PublicKey{pub}); err != nil {
		t.Fatalf("the index does not verify with its own key: %v", err)
	}
	other, _ := testKey(t)
	if _, err := update.VerifyReleaseIndex(body, sig, []ed25519.PublicKey{other}); err == nil {
		t.Fatal("the index verified with another key")
	}
}

func TestReleaseIndexRefusals(t *testing.T) {
	_, priv := testKey(t)
	dir := fixtureRelease(t, priv)
	if _, _, _, err := buildReleaseIndex(BuildIndexOptions{Dir: dir, Version: "v0.0.1", Key: priv}); err == nil {
		t.Fatal("an index for another version built")
	}
	if _, _, _, err := buildReleaseIndex(BuildIndexOptions{Dir: dir, Version: fixtureVersion}); err == nil {
		t.Fatal("an index built without a key")
	}
	// An archive without a signature is refused, as for the manifest.
	minimal := update.DistroArtifactName(fixtureVersion, "minimal", "linux", "amd64")
	writeTarGz(t, filepath.Join(dir, minimal), []tarMember{{name: "observer", kind: tar.TypeReg, content: "x"}})
	writeSums(t, dir)
	if _, _, _, err := buildReleaseIndex(BuildIndexOptions{Dir: dir, Version: fixtureVersion, Key: priv}); err == nil {
		t.Fatal("an unsigned distro archive was indexed")
	}
}
