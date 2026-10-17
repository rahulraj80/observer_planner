package update

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// distro_v2_test.go covers modular packaging plan 5.6 / 5.6.1 on the pure
// side (lane MP-REL2, T-687): the v2 manifest schema, the vendor-signed
// release index, distro archive names and the three distro verify rules.
// Every key here is a THROWAWAY derived from a fixed phrase; no real vendor
// or org key is used or needed.

func testVendorKey(phrase string) (ed25519.PrivateKey, ed25519.PublicKey) {
	seed := sha256.Sum256([]byte("mp-rel2 test vendor key " + phrase))
	priv := ed25519.NewKeyFromSeed(seed[:])
	return priv, priv.Public().(ed25519.PublicKey)
}

func hex64(c byte) string { return strings.Repeat(string(c), 64) }

// distroArtifact is one v2 artifact for linux/amd64.
func distroArtifact(distro string, sha, digest byte) Artifact {
	a := linuxArtifact()
	a.Filename = DistroArtifactName("v1.36.0", distro, "linux", "amd64")
	a.SHA256 = hex64(sha)
	a.Distro = distro
	a.ComponentsDigest = hex64(digest)
	return a
}

func v2Manifest(epoch int64, arts ...Artifact) Manifest {
	m := baseManifest()
	m.Schema = SchemaV2
	m.Version = "v1.36.0"
	m.MinFromVersion = ""
	m.MinServerVersion = ""
	m.NotesURL = ""
	m.ManifestVersion = 40
	m.DistroEpoch = epoch
	m.Artifacts = arts
	return m
}

func indexFor(t *testing.T, version string, arts ...Artifact) []byte {
	t.Helper()
	ix := ReleaseIndex{Schema: ReleaseIndexSchemaV1, Version: version, CatalogVersion: components.CatalogVersion}
	for _, a := range arts {
		ix.Artifacts = append(ix.Artifacts, ReleaseIndexEntry{
			Filename: a.Filename, Kind: a.Kind, Distro: NormalizeDistro(a.Distro), OS: a.OS, Arch: a.Arch,
			ArchiveType: a.ArchiveType, SizeBytes: a.SizeBytes, SHA256: a.SHA256,
			Member: a.Member, MemberSHA256: a.MemberSHA256, AliasMembers: a.AliasMembers,
			ComponentsDigest: a.ComponentsDigest,
		})
	}
	b, err := CanonicalReleaseIndex(ix)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// signV2 builds the v2 envelope with the golden org key under the v2
// domain, plus the index signed by the vendor key.
func signV2(t *testing.T, m Manifest, index []byte, vendor ed25519.PrivateKey) EnvelopeV2 {
	t.Helper()
	priv, key := goldenKey()
	m.ReleaseIndexSHA256 = HashBytes(index)
	raw, err := CanonicalJSON(m)
	if err != nil {
		t.Fatal(err)
	}
	h := HashBytes(raw)
	sig, err := SignReleaseIndex(vendor, index)
	if err != nil {
		t.Fatal(err)
	}
	return EnvelopeV2{
		Envelope: Envelope{
			ManifestB64: base64.StdEncoding.EncodeToString(raw), ManifestHash: h, KeyID: key.KeyID,
			Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, ManifestSigningMessageV2(m.ManifestVersion, h))),
		},
		ReleaseIndexB64: base64.StdEncoding.EncodeToString(index),
		ReleaseIndexSig: sig,
	}
}

func TestKnownDistrosMatchTheCatalog(t *testing.T) {
	var want []string
	for _, d := range components.Distros() {
		want = append(want, string(d))
	}
	if got := KnownDistros(); !reflect.DeepEqual(got, want) {
		t.Fatalf("KnownDistros = %v, catalog = %v", got, want)
	}
	if NormalizeDistro("") != DistroFull || !KnownDistro("") || KnownDistro("custom") {
		t.Fatal("empty must mean full; custom is not a prebuilt distro")
	}
}

func TestDistroArtifactNames(t *testing.T) {
	cases := []struct {
		name                     string
		version, distro, os, arc string
		explicit                 bool
		wantErr                  bool
	}{
		{name: "observer-v1.36.0-linux-x64.tar.gz", version: "v1.36.0", distro: "full", os: "linux", arc: "amd64"},
		{name: "observer-v1.36.0-minimal-linux-x64.tar.gz", version: "v1.36.0", distro: "minimal", os: "linux", arc: "amd64", explicit: true},
		{name: "observer-v1.36.0-managed-win32-x64.zip", version: "v1.36.0", distro: "managed", os: "windows", arc: "amd64", explicit: true},
		{name: "observer-v1.36.0-rc.2-developer-darwin-arm64.tar.gz", version: "v1.36.0-rc.2", distro: "developer", os: "darwin", arc: "arm64", explicit: true},
		{name: "observer-v1.36.0-rc.2-linux-x64.tar.gz", version: "v1.36.0-rc.2", distro: "full", os: "linux", arc: "amd64"},
		{name: "observer-v1.36.0-rc.2-tiny-linux-x64.tar.gz", wantErr: true},
		{name: "observer-v1.36.0-minimal-win32-x64.tar.gz", wantErr: true},
	}
	for _, c := range cases {
		n, err := ParseDistroArtifactName(c.name)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: want error, got %+v", c.name, n)
			}
			continue
		}
		if err != nil || n.Version != c.version || n.Distro != c.distro || n.OS != c.os || n.Arch != c.arc || n.ExplicitDistro != c.explicit {
			t.Errorf("%s: got %+v, %v", c.name, n, err)
		}
		if got := DistroArtifactName(n.Version, n.Distro, n.OS, n.Arch); got != c.name {
			t.Errorf("round trip %s -> %s", c.name, got)
		}
	}
	// The v1 parser refuses a distro archive so a scanner skips it and no
	// version is ever recorded as "v1.36.0-minimal".
	if _, _, _, _, err := ParseArtifactName("observer-v1.36.0-minimal-linux-x64.tar.gz"); !errors.Is(err, ErrUnrecognizedArtifact) || !errors.Is(err, ErrDistroArtifact) {
		t.Fatalf("ParseArtifactName on a distro archive: %v", err)
	}
	if v, _, _, _, err := ParseArtifactName("observer-v1.36.0-linux-x64.tar.gz"); err != nil || v != "v1.36.0" {
		t.Fatalf("today's name: %q %v", v, err)
	}
}

func TestValidateSchemasKeepTheirOwnFields(t *testing.T) {
	v1 := baseManifest()
	if err := Validate(v1); err != nil {
		t.Fatalf("v1: %v", err)
	}
	bad := baseManifest()
	bad.Artifacts[0].Distro = "minimal"
	if err := Validate(bad); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("v1 naming minimal: %v", err)
	}
	bad = baseManifest()
	bad.DistroEpoch = 3
	if err := Validate(bad); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("v1 with an epoch: %v", err)
	}
	m := v2Manifest(1, distroArtifact("full", '5', 'a'), distroArtifact("minimal", '6', 'b'))
	m.ReleaseIndexSHA256 = hex64('c')
	if err := Validate(m); err != nil {
		t.Fatalf("v2: %v", err)
	}
	noDigest := m
	noDigest.Artifacts = []Artifact{distroArtifact("full", '5', 'a')}
	noDigest.Artifacts[0].ComponentsDigest = ""
	if err := Validate(noDigest); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("v2 without a components digest: %v", err)
	}
	dup := m
	dup.Artifacts = []Artifact{distroArtifact("minimal", '5', 'a'), distroArtifact("minimal", '6', 'b')}
	dup.Artifacts[1].Filename = "observer-v1.36.0-minimal-linux-x64-copy.tar.gz"
	if err := Validate(dup); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("v2 duplicate distro key: %v", err)
	}
}

func TestReleaseIndexSignature(t *testing.T) {
	vPriv, vPub := testVendorKey("a")
	_, otherPub := testVendorKey("b")
	full := distroArtifact("full", '5', 'a')
	body := indexFor(t, "v1.36.0", full)
	sig, err := SignReleaseIndex(vPriv, body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyReleaseIndex(body, sig, []ed25519.PublicKey{otherPub, vPub}); err != nil {
		t.Fatalf("rotation overlap: %v", err)
	}
	if _, err := VerifyReleaseIndex(body, sig, []ed25519.PublicKey{otherPub}); !errors.Is(err, ErrReleaseIndexInvalid) {
		t.Fatalf("wrong key: %v", err)
	}
	if _, err := VerifyReleaseIndex(body, sig, nil); !errors.Is(err, ErrNoVendorKey) {
		t.Fatalf("no key must refuse, not skip: %v", err)
	}
	tampered := []byte(strings.Replace(string(body), `"distro":"full"`, `"distro":"minimal"`, 1))
	if _, err := VerifyReleaseIndex(tampered, sig, []ed25519.PublicKey{vPub}); err == nil {
		t.Fatal("a tampered index verified")
	}
	// The domain tag keeps an archive-style raw signature over the bytes
	// from verifying as an index signature.
	raw := base64.StdEncoding.EncodeToString(ed25519.Sign(vPriv, body))
	if _, err := VerifyReleaseIndex(body, raw, []ed25519.PublicKey{vPub}); err == nil {
		t.Fatal("a raw signature over the bytes verified as an index signature")
	}
	ix, _ := DecodeReleaseIndex(body)
	lie := full
	lie.ComponentsDigest = hex64('b')
	if err := CheckArtifactAgainstIndex(ix, lie); !errors.Is(err, ErrReleaseIndexMismatch) {
		t.Fatalf("digest lie: %v", err)
	}
	moved := full
	moved.Distro = "minimal"
	if err := CheckArtifactAgainstIndex(ix, moved); !errors.Is(err, ErrReleaseIndexMismatch) {
		t.Fatalf("distro lie: %v", err)
	}
}

// TestVerifyV2DistroRules walks the distro rules one row at a time.
func TestVerifyV2DistroRules(t *testing.T) {
	vPriv, vPub := testVendorKey("a")
	otherPriv, _ := testVendorKey("b")
	full := distroArtifact("full", '5', 'a')
	minimal := distroArtifact("minimal", '6', 'b')
	index := indexFor(t, "v1.36.0", full, minimal)
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	base := func(installed string, d DistroContext) VerifyInput {
		return VerifyInput{
			PinnedPublicKey: goldenPubKey(t), PinnedKeyID: goldenKeyID(t),
			InstalledVersion: installed, Now: now, GOOS: "linux", GOARCH: "amd64",
			Schema: SchemaV2, VendorKeys: []ed25519.PublicKey{vPub}, Distro: d,
		}
	}
	aware := func(cur, target string) DistroContext {
		return DistroContext{Aware: true, Current: cur, Target: target, UpdatesAllowed: true, TargetAllowed: true, Epoch: 1}
	}
	type tc struct {
		name       string
		env        EnvelopeV2
		in         VerifyInput
		wantRule   string
		wantState  State
		wantReason Reason
		wantDistro string
		transition bool
	}
	good := signV2(t, v2Manifest(2, full, minimal), index, vPriv)
	cases := []tc{
		{name: "same distro update", env: good, in: base("v1.35.0", aware("full", "")), wantRule: "verified", wantState: StateAvailable, wantDistro: "full"},
		{name: "minimal node gets minimal", env: good, in: base("v1.35.0", aware("minimal", "")), wantRule: "verified", wantState: StateAvailable, wantDistro: "minimal"},
		{name: "transition full to minimal", env: good, in: base("v1.35.0", aware("full", "minimal")), wantRule: "verified", wantState: StateAvailable, wantDistro: "minimal", transition: true},
		{name: "same-version transition is its own rule", env: good, in: base("v1.36.0", aware("full", "minimal")), wantRule: "verified", wantState: StateAvailable, wantDistro: "minimal", transition: true},
		{name: "same version without a transition is not an update", env: good, in: base("v1.36.0", aware("full", "")), wantRule: "7-downgrade", wantState: StateBlocked, wantReason: ReasonDowngrade},
		{name: "lower version transition is still a downgrade", env: good, in: base("v1.37.0", aware("full", "minimal")), wantRule: "7-downgrade", wantState: StateBlocked, wantReason: ReasonDowngrade},
		{name: "transition not allowed", env: good, in: func() VerifyInput {
			d := aware("full", "minimal")
			d.TargetAllowed = false
			return base("v1.35.0", d)
		}(), wantRule: "12-distro", wantState: StateBlocked, wantReason: ReasonDistroNotAllowed},
		{name: "transition needs a newer epoch", env: good, in: func() VerifyInput {
			d := aware("full", "minimal")
			d.Epoch = 2
			return base("v1.35.0", d)
		}(), wantRule: "12-distro", wantState: StateBlocked, wantReason: ReasonDistroEpochStale},
		{name: "ceiling invalid refuses every update", env: good, in: func() VerifyInput {
			d := aware("full", "")
			d.UpdatesAllowed = false
			return base("v1.35.0", d)
		}(), wantRule: "10-ceiling", wantState: StateBlocked, wantReason: ReasonCeilingInvalid},
		{name: "unpublished target distro", env: good, in: base("v1.35.0", aware("full", "managed")), wantRule: "9-artifact", wantState: StateBlocked, wantReason: ReasonNoArtifact},
		{name: "index signed by another key", env: signV2(t, v2Manifest(2, full, minimal), index, otherPriv), in: base("v1.35.0", aware("full", "")), wantRule: "11-release-index", wantState: StateBlocked, wantReason: ReasonReleaseIndex},
		{name: "no vendor key refuses", env: good, in: func() VerifyInput {
			in := base("v1.35.0", aware("full", ""))
			in.VendorKeys = nil
			return in
		}(), wantRule: "11-release-index", wantState: StateBlocked, wantReason: ReasonReleaseIndex},
		{name: "index swapped after signing", env: func() EnvelopeV2 {
			e := good
			other := indexFor(t, "v1.36.0", full)
			e.ReleaseIndexB64 = base64.StdEncoding.EncodeToString(other)
			e.ReleaseIndexSig, _ = SignReleaseIndex(vPriv, other)
			return e
		}(), in: base("v1.35.0", aware("full", "")), wantRule: "11-release-index", wantState: StateBlocked, wantReason: ReasonReleaseIndex},
		{name: "org claims full archive is minimal", env: func() EnvelopeV2 {
			liar := minimal
			liar.SHA256 = full.SHA256
			return signV2(t, v2Manifest(2, full, liar), index, vPriv)
		}(), in: base("v1.35.0", aware("full", "minimal")), wantRule: "11-release-index", wantState: StateBlocked, wantReason: ReasonReleaseIndex},
		{name: "v2 document on the v1 rail is ignored", env: good, in: func() VerifyInput {
			in := base("v1.35.0", DistroContext{})
			in.Schema = ""
			return in
		}(), wantRule: "4-signature", wantState: StateIdle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := c.in
			in.Envelope = c.env.Envelope
			in.ReleaseIndexB64, in.ReleaseIndexSig = c.env.ReleaseIndexB64, c.env.ReleaseIndexSig
			res, err := Verify(in)
			if c.wantRule == "verified" && err != nil {
				t.Fatalf("err = %v", err)
			}
			if res.Rule != c.wantRule || res.State != c.wantState || res.Reason != c.wantReason {
				t.Fatalf("rule %q state %q reason %q (err %v), want %q %q %q", res.Rule, res.State, res.Reason, err, c.wantRule, c.wantState, c.wantReason)
			}
			if c.wantDistro != "" && res.Distro != c.wantDistro {
				t.Fatalf("distro = %q, want %q", res.Distro, c.wantDistro)
			}
			if res.DistroTransition != c.transition {
				t.Fatalf("transition = %v, want %v", res.DistroTransition, c.transition)
			}
		})
	}
}

// TestVerifyV1ForADistroAwareNode is the new-node / old-server cell: the v1
// document is the full build, taken only by a full node whose ceiling
// permits full.
func TestVerifyV1ForADistroAwareNode(t *testing.T) {
	raw := loadEnvelope(t, "valid")
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	run := func(d DistroContext) VerifyResult {
		res, _ := Verify(VerifyInput{
			EnvelopeBytes: raw, PinnedPublicKey: goldenPubKey(t), PinnedKeyID: goldenKeyID(t),
			InstalledVersion: "v1.30.0", Now: now, GOOS: "linux", GOARCH: "amd64", Distro: d,
		})
		return res
	}
	if res := run(DistroContext{}); res.State != StateAvailable {
		t.Fatalf("an unaware node keeps today's behaviour: %+v", res)
	}
	ok := DistroContext{Aware: true, Current: "full", UpdatesAllowed: true, LegacyFullAllowed: true}
	if res := run(ok); res.State != StateAvailable || res.Distro != DistroFull {
		t.Fatalf("full node, full allowed: %+v", res)
	}
	for name, d := range map[string]DistroContext{
		"minimal node":            {Aware: true, Current: "minimal", UpdatesAllowed: true, LegacyFullAllowed: true},
		"policy names developer":  {Aware: true, Current: "full", Target: "developer", UpdatesAllowed: true, LegacyFullAllowed: true},
		"ceiling does not permit": {Aware: true, Current: "full", UpdatesAllowed: true},
	} {
		if res := run(d); res.State != StateBlocked || res.Reason != ReasonServerLacksDistroUpdates {
			t.Errorf("%s: %+v", name, res)
		}
	}
}
