//go:build !sbo_omit_org_update

package orgclient

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/update"
)

// update_distro_test.go is the four-cell compatibility matrix of modular
// packaging plan 5.6.1 (third-pass M2), owned by this file per plan 11 row
// MP-REL (lane MP-REL2, T-687):
//
//	                 old server (v1 route only)        new server (v1 + v2)
//	old node   today's behaviour                  v1 route, full build, unchanged
//	new node   v2 404 -> v1 under rule 12         v2 with the release index
//
// An "old node" is a client with no distro source: it never calls v2 and runs
// no distro rule, exactly the pre-modular binary. An "old server" serves only
// /api/agent/update/manifest. Keys are throwaway.

// fakeUpdateServer serves the v1 document and, when v2 is set, the v2 one.
type fakeUpdateServer struct {
	mu    sync.Mutex
	v1    update.Envelope
	v2    *update.EnvelopeV2
	paths []string
}

func (f *fakeUpdateServer) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.paths = append(f.paths, r.URL.Path)
	f.mu.Unlock()
	switch r.URL.Path {
	case "/api/agent/update/manifest":
		writeTestJSON(w, http.StatusOK, f.v1)
	case "/api/agent/update/manifest/v2":
		if f.v2 == nil {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(w, http.StatusOK, *f.v2)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeUpdateServer) calledV2() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.paths {
		if strings.HasSuffix(p, "/v2") {
			return true
		}
	}
	return false
}

// distroTestFixture builds one release (v1.36.0) published both ways.
type distroTestFixture struct {
	keyID      string
	v1         update.Envelope
	v2         update.EnvelopeV2
	vendorKeys []ed25519.PublicKey
}

func newDistroTestFixture(t *testing.T) distroTestFixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	keyID := base64.StdEncoding.EncodeToString(pub)
	seed := sha256.Sum256([]byte("orgclient distro test vendor key"))
	vendor := ed25519.NewKeyFromSeed(seed[:])
	v1 := testManifest("v1.36.0", 30)

	goos, goarch := update.CurrentPlatform()
	full := v1.Artifacts[0]
	full.Distro, full.ComponentsDigest = "full", strings.Repeat("d", 64)
	minimal := full
	minimal.Distro, minimal.ComponentsDigest = "minimal", strings.Repeat("e", 64)
	minimal.Filename = update.DistroArtifactName("v1.36.0", "minimal", goos, goarch)
	minimal.SHA256 = strings.Repeat("f", 64)
	ix := update.ReleaseIndex{Schema: update.ReleaseIndexSchemaV1, Version: "v1.36.0", CatalogVersion: 1}
	for _, a := range []update.Artifact{full, minimal} {
		ix.Artifacts = append(ix.Artifacts, update.ReleaseIndexEntry{
			Filename: a.Filename, Kind: a.Kind, Distro: a.Distro, OS: a.OS, Arch: a.Arch,
			ArchiveType: a.ArchiveType, SizeBytes: a.SizeBytes, SHA256: a.SHA256,
			Member: a.Member, MemberSHA256: a.MemberSHA256, AliasMembers: a.AliasMembers,
			ComponentsDigest: a.ComponentsDigest,
		})
	}
	body, err := update.CanonicalReleaseIndex(ix)
	if err != nil {
		t.Fatal(err)
	}
	isig, err := update.SignReleaseIndex(vendor, body)
	if err != nil {
		t.Fatal(err)
	}
	m2 := v1
	m2.Schema = update.SchemaV2
	m2.Artifacts = []update.Artifact{full, minimal}
	update.SortArtifacts(m2.Artifacts)
	m2.ReleaseIndexSHA256 = update.HashBytes(body)
	m2.DistroEpoch = 1
	canonical, err := update.CanonicalJSON(m2)
	if err != nil {
		t.Fatal(err)
	}
	h := update.HashBytes(canonical)
	return distroTestFixture{
		keyID: keyID,
		v1:    signedManifest(t, priv, keyID, v1),
		v2: update.EnvelopeV2{
			Envelope: update.Envelope{
				ManifestB64: base64.StdEncoding.EncodeToString(canonical), ManifestHash: h, KeyID: keyID,
				Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, update.ManifestSigningMessageV2(m2.ManifestVersion, h))),
			},
			ReleaseIndexB64: base64.StdEncoding.EncodeToString(body),
			ReleaseIndexSig: isig,
		},
		vendorKeys: []ed25519.PublicKey{vendor.Public().(ed25519.PublicKey)},
	}
}

func TestUpdateNegotiationFourCells(t *testing.T) {
	fx := newDistroTestFixture(t)
	fullNode := func(context.Context, update.Channel) update.DistroContext {
		return update.DistroContext{Current: "full", UpdatesAllowed: true, LegacyFullAllowed: true}
	}
	minimalNode := func(context.Context, update.Channel) update.DistroContext {
		return update.DistroContext{Current: "minimal", UpdatesAllowed: true, LegacyFullAllowed: false}
	}
	cases := []struct {
		name       string
		newServer  bool
		node       func(context.Context, update.Channel) update.DistroContext
		wantState  update.State
		wantReason update.Reason
		wantDistro string
		wantV2Call bool
		wantSchema string
	}{
		{
			name: "old node, old server", newServer: false, node: nil,
			wantState: update.StateAvailable, wantDistro: "full", wantSchema: update.SchemaV1,
		},
		{
			name: "old node, new server", newServer: true, node: nil,
			wantState: update.StateAvailable, wantDistro: "full", wantSchema: update.SchemaV1,
		},
		{
			name: "new full node, old server", newServer: false, node: fullNode,
			wantState: update.StateAvailable, wantDistro: "full", wantV2Call: true, wantSchema: update.SchemaV1,
		},
		{
			name: "new minimal node, old server", newServer: false, node: minimalNode,
			wantState: update.StateBlocked, wantReason: update.ReasonServerLacksDistroUpdates, wantV2Call: true,
		},
		{
			name: "new full node, new server", newServer: true, node: fullNode,
			wantState: update.StateAvailable, wantDistro: "full", wantV2Call: true, wantSchema: update.SchemaV2,
		},
		{
			name: "new minimal node, new server", newServer: true, node: minimalNode,
			wantState: update.StateAvailable, wantDistro: "minimal", wantV2Call: true, wantSchema: update.SchemaV2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := &fakeUpdateServer{v1: fx.v1}
			if tc.newServer {
				v2 := fx.v2
				fs.v2 = &v2
			}
			srv := httptest.NewServer(http.HandlerFunc(fs.handler))
			defer srv.Close()
			s := newAgentStore(t)
			enrol(t, s, srv.URL)
			c := newTestClient(t, s, &memBearerStore{bearer: "b"})
			c.agentVersion = "v1.35.0"
			c.setUpdateVendorKeys(fx.vendorKeys)
			if tc.node != nil {
				c.SetUpdateDistroSource(tc.node)
			}
			if _, err := c.FetchOrgUpdateManifest(context.Background(), update.ChannelStable); err != nil {
				t.Fatalf("fetch: %v", err)
			}
			st := c.UpdateStatus()
			if st.State != tc.wantState || st.Reason != tc.wantReason {
				t.Fatalf("state %q reason %q, want %q %q", st.State, st.Reason, tc.wantState, tc.wantReason)
			}
			if fs.calledV2() != tc.wantV2Call {
				t.Fatalf("v2 called = %v, want %v (an old node must never call v2)", fs.calledV2(), tc.wantV2Call)
			}
			if tc.wantDistro != "" && st.Distro != tc.wantDistro {
				t.Fatalf("distro = %q, want %q", st.Distro, tc.wantDistro)
			}
			if tc.wantSchema != "" && st.Schema != tc.wantSchema {
				t.Fatalf("schema = %q, want %q", st.Schema, tc.wantSchema)
			}
			if tc.node == nil && (st.Artifact.Distro != "" || st.Artifact.ComponentsDigest != "") {
				t.Fatalf("an old node saw v2 fields: %+v", st.Artifact)
			}
		})
	}
}

// TestUpdateRailHonoursTheCeiling: while the local ceiling's source fails a
// check, every update is refused on both rails and the posture says why.
func TestUpdateRailHonoursTheCeiling(t *testing.T) {
	fx := newDistroTestFixture(t)
	for _, newServer := range []bool{false, true} {
		fs := &fakeUpdateServer{v1: fx.v1}
		if newServer {
			v2 := fx.v2
			fs.v2 = &v2
		}
		srv := httptest.NewServer(http.HandlerFunc(fs.handler))
		s := newAgentStore(t)
		enrol(t, s, srv.URL)
		c := newTestClient(t, s, &memBearerStore{bearer: "b"})
		c.agentVersion = "v1.35.0"
		c.setUpdateVendorKeys(fx.vendorKeys)
		c.SetUpdateDistroSource(func(context.Context, update.Channel) update.DistroContext {
			return update.DistroContext{Current: "full", UpdatesAllowed: false, LegacyFullAllowed: true}
		})
		if _, err := c.FetchOrgUpdateManifest(context.Background(), update.ChannelStable); err != nil {
			t.Fatal(err)
		}
		if st := c.UpdateStatus(); st.State != update.StateBlocked || st.Reason != update.ReasonCeilingInvalid {
			t.Errorf("new server %v: %+v", newServer, st)
		}
		srv.Close()
	}
}

// TestV2IndexFromTheWrongVendorKeyIsBlocked: the org cannot pass off its own
// index; a v2 document whose index does not verify is a blocked posture.
func TestV2IndexFromTheWrongVendorKeyIsBlocked(t *testing.T) {
	fx := newDistroTestFixture(t)
	v2 := fx.v2
	fs := &fakeUpdateServer{v1: fx.v1, v2: &v2}
	srv := httptest.NewServer(http.HandlerFunc(fs.handler))
	defer srv.Close()
	s := newAgentStore(t)
	enrol(t, s, srv.URL)
	c := newTestClient(t, s, &memBearerStore{bearer: "b"})
	c.agentVersion = "v1.35.0"
	other := sha256.Sum256([]byte("not the vendor"))
	c.setUpdateVendorKeys([]ed25519.PublicKey{ed25519.NewKeyFromSeed(other[:]).Public().(ed25519.PublicKey)})
	c.SetUpdateDistroSource(func(context.Context, update.Channel) update.DistroContext {
		return update.DistroContext{Current: "minimal", UpdatesAllowed: true}
	})
	if _, err := c.FetchOrgUpdateManifest(context.Background(), update.ChannelStable); err != nil {
		t.Fatal(err)
	}
	if st := c.UpdateStatus(); st.State != update.StateBlocked || st.Reason != update.ReasonReleaseIndex {
		t.Fatalf("%+v", st)
	}
}
