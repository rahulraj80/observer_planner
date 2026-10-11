package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/artifactstate"
	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/govern"
)

// TestComponentsEndpointServesCatalog pins GET /api/components: it answers
// the catalog report for the node binary, with every node row and no
// server row unless asked.
func TestComponentsEndpointServesCatalog(t *testing.T) {
	s := newRemoteTestServer(t, Options{})
	h := s.guardedHandler("127.0.0.1:8081")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/components"))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/components = %d: %s", rec.Code, rec.Body.String())
	}
	var rep components.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Binary != components.BinaryNode || rep.CatalogVersion != components.CatalogVersion {
		t.Fatalf("report header = %s v%d", rep.Binary, rep.CatalogVersion)
	}
	if len(rep.Components) != len(components.ForBinary(components.BinaryNode)) {
		t.Fatalf("components = %d, want every node row", len(rep.Components))
	}
	for _, c := range rep.Components {
		if c.Binary != components.BinaryNode {
			t.Fatalf("server row %s leaked into the default report", c.ID)
		}
	}
	if rep.ComponentsDigest == "" || len(rep.Artifacts) == 0 {
		t.Fatal("digest and artifacts are part of the report")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/components?all=1"))
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Components) != len(components.All()) {
		t.Fatalf("?all=1 components = %d, want %d", len(rep.Components), len(components.All()))
	}
}

// TestComponentsEndpointCarriesArtifactStates (T-707): with the artifact
// observer wired, /api/components adds artifact_states beside the unchanged
// catalog, with the item paths removed; unwired, the field is absent.
func TestComponentsEndpointCarriesArtifactStates(t *testing.T) {
	states := []artifactstate.Status{{
		ID: "artifact.vscode-extension", State: artifactstate.StateStale, Version: "1.34.0", Detail: "older than this observer",
		Items: []artifactstate.Item{{Name: "Cursor", Path: "/home/u/.cursor/extensions/x", State: "stale", Version: "1.34.0"}},
	}}
	s := newRemoteTestServer(t, Options{ComponentArtifacts: func(context.Context) []artifactstate.Status { return states }})
	h := s.guardedHandler("127.0.0.1:8081")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/components"))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/components = %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		components.Report
		ArtifactStates []artifactstate.Status `json:"artifact_states"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Binary != components.BinaryNode || len(got.Components) != len(components.ForBinary(components.BinaryNode)) {
		t.Fatalf("the catalog fields changed shape: %s %d", got.Binary, len(got.Components))
	}
	if len(got.ArtifactStates) != 1 || got.ArtifactStates[0].State != artifactstate.StateStale || len(got.ArtifactStates[0].Items) != 1 {
		t.Fatalf("artifact_states = %+v", got.ArtifactStates)
	}
	if got.ArtifactStates[0].Items[0].Path != "" || states[0].Items[0].Path == "" {
		t.Fatal("an item path reached the dashboard (or the provider's slice was mutated)")
	}

	plain := newRemoteTestServer(t, Options{})
	rec = httptest.NewRecorder()
	plain.guardedHandler("127.0.0.1:8081").ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/components"))
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["artifact_states"]; ok {
		t.Fatal("an unwired server reported artifact_states")
	}
}

// TestComponentsEndpointReadOnly: the route is a read; any write verb is
// refused, and it is VIEW + SectionNone like /api/governance.
func TestComponentsEndpointReadOnly(t *testing.T) {
	s := newRemoteTestServer(t, Options{})
	h := s.guardedHandler("127.0.0.1:8081")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, loopbackRequest(http.MethodPost, "/api/components"))
	if rec.Code == http.StatusOK {
		t.Fatal("POST /api/components must not succeed")
	}
	_, caps, sections := s.registerRoutes(nil)
	if caps["/api/components"] != CapabilityView {
		t.Errorf("capability = %v, want View", caps["/api/components"])
	}
	if sections["/api/components"] != SectionNone {
		t.Errorf("section = %v, want SectionNone", sections["/api/components"])
	}
}

// TestComponentsEndpointNeverHidden: governance that hides every hideable
// section still cannot conceal the catalog.
func TestComponentsEndpointNeverHidden(t *testing.T) {
	eff := govern.Effective{
		Active: true, State: govern.StateApplied, Version: 3,
		HiddenSections: []string{"settings", "privacy", "benchmarks", "terminals", "security"},
	}
	s := newRemoteTestServer(t, Options{Governance: func(context.Context) govern.Effective { return eff }})
	rec := httptest.NewRecorder()
	s.guardedHandler("127.0.0.1:8081").ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/components"))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/components under governance = %d", rec.Code)
	}
}
