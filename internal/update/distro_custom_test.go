package update

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// distro_custom_test.go covers the custom build on the update rail
// (modular packaging plan 5.3, lane MP-CUST): a custom distro id is
// `custom-<64 hex manifest digest>`, a custom node updates only to its own
// manifest's build named by the vendor-signed index, and no other node ever
// selects a custom artifact. Throwaway keys only.

func TestCustomDistroVocabularyMatchesTheCatalog(t *testing.T) {
	if CustomDistroPrefix != components.CustomDistroPrefix {
		t.Fatalf("prefix %q vs catalog %q", CustomDistroPrefix, components.CustomDistroPrefix)
	}
	d := CustomDistro(hex64('a'))
	if !IsCustomDistro(d) || !components.IsCustomDistro(d) {
		t.Fatalf("%q is not a custom distro on both sides", d)
	}
	for _, bad := range []string{"custom", "custom-", "custom-" + hex64('a')[:16], "custom-" + hex64('g'), "minimal"} {
		if IsCustomDistro(bad) {
			t.Errorf("%q accepted as a custom distro", bad)
		}
	}
	if KnownDistro(d) || !ArtifactDistro(d) || !ArtifactDistro("minimal") || ArtifactDistro("tiny") {
		t.Fatal("ArtifactDistro must be prebuilt or custom, KnownDistro prebuilt only")
	}
}

func TestCustomArtifactNames(t *testing.T) {
	d := CustomDistro(hex64('b'))
	for _, c := range []struct{ version, os, arch string }{
		{"v1.36.0", "linux", "amd64"}, {"v1.36.0-rc.2", "windows", "amd64"}, {"v1.36.0", "darwin", "arm64"},
	} {
		name := DistroArtifactName(c.version, d, c.os, c.arch)
		n, err := ParseDistroArtifactName(name)
		if err != nil || n.Version != c.version || n.Distro != d || n.OS != c.os || n.Arch != c.arch || !n.ExplicitDistro {
			t.Fatalf("%s: %+v %v", name, n, err)
		}
		if _, _, _, _, err := ParseArtifactName(name); !errors.Is(err, ErrDistroArtifact) {
			t.Fatalf("the v1 parser must refuse a custom archive: %v", err)
		}
	}
	if _, err := ParseDistroArtifactName("observer-v1.36.0-" + d + "-linux-x64.zip"); !errors.Is(err, ErrArchiveTypeMismatch) {
		t.Fatalf("a custom zip for linux: %v", err)
	}
	if _, err := ParseDistroArtifactName("observer-v1.36.0-custom-abc-linux-x64.tar.gz"); err == nil {
		t.Fatal("a truncated custom id parsed")
	}
}

// TestVerifyCustomBuildUpdatesOnlyFromItsOwnManifest walks the custom rows.
func TestVerifyCustomBuildUpdatesOnlyFromItsOwnManifest(t *testing.T) {
	vPriv, vPub := testVendorKey("custom")
	mine := CustomDistro(hex64('1'))
	theirs := CustomDistro(hex64('2'))
	full := distroArtifact("full", '5', 'a')
	myBuild := distroArtifact(mine, '6', 'b')
	theirBuild := distroArtifact(theirs, '7', 'c')
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	in := func(cur string, env EnvelopeV2) VerifyInput {
		return VerifyInput{
			PinnedPublicKey: goldenPubKey(t), PinnedKeyID: goldenKeyID(t),
			InstalledVersion: "v1.35.0", Now: now, GOOS: "linux", GOARCH: "amd64",
			Schema: SchemaV2, VendorKeys: []ed25519.PublicKey{vPub},
			Distro:   DistroContext{Aware: true, Current: cur, UpdatesAllowed: true, TargetAllowed: true, Epoch: 1},
			Envelope: env.Envelope, ReleaseIndexB64: env.ReleaseIndexB64, ReleaseIndexSig: env.ReleaseIndexSig,
		}
	}
	all := signV2(t, v2Manifest(2, full, myBuild, theirBuild), indexFor(t, "v1.36.0", full, myBuild, theirBuild), vPriv)

	res, err := Verify(in(mine, all))
	if err != nil || res.State != StateAvailable || res.Distro != mine || res.Artifact.SHA256 != myBuild.SHA256 {
		t.Fatalf("custom node, own build published: %+v %v", res, err)
	}
	// A full node never selects a custom artifact.
	if res, _ := Verify(in("full", all)); res.Distro != DistroFull || res.Artifact.SHA256 != full.SHA256 {
		t.Fatalf("full node: %+v", res)
	}
	// Only another manifest's build (and full) published: refuse, no fallback.
	others := signV2(t, v2Manifest(2, full, theirBuild), indexFor(t, "v1.36.0", full, theirBuild), vPriv)
	res, err = Verify(in(mine, others))
	if res.State != StateBlocked || res.Reason != ReasonNoArtifact || !errors.Is(err, ErrNoCustomArtifact) || !errors.Is(err, ErrNoArtifact) {
		t.Fatalf("custom node without its build: %+v %v", res, err)
	}
	// The org lists my build but the vendor index does not: refuse.
	unvouched := signV2(t, v2Manifest(2, full, myBuild), indexFor(t, "v1.36.0", full), vPriv)
	if res, _ := Verify(in(mine, unvouched)); res.State != StateBlocked || res.Reason != ReasonReleaseIndex {
		t.Fatalf("custom build missing from the index: %+v", res)
	}
	// The org relabels another manifest's archive as mine: the index says
	// otherwise, refuse.
	liar := theirBuild
	liar.Distro, liar.Filename = mine, myBuild.Filename
	relabel := signV2(t, v2Manifest(2, full, liar), indexFor(t, "v1.36.0", full, myBuild), vPriv)
	if res, _ := Verify(in(mine, relabel)); res.State != StateBlocked || res.Reason != ReasonReleaseIndex {
		t.Fatalf("relabelled archive: %+v", res)
	}
	// A v1 manifest (an old server) offers only full: a custom node refuses.
	raw := loadEnvelope(t, "valid")
	v1, _ := Verify(VerifyInput{
		EnvelopeBytes: raw, PinnedPublicKey: goldenPubKey(t), PinnedKeyID: goldenKeyID(t),
		InstalledVersion: "v1.30.0", Now: now, GOOS: "linux", GOARCH: "amd64",
		Distro: DistroContext{Aware: true, Current: mine, UpdatesAllowed: true, LegacyFullAllowed: true},
	})
	if v1.State != StateBlocked || v1.Reason != ReasonServerLacksDistroUpdates {
		t.Fatalf("custom node on a v1 server: %+v", v1)
	}
}
