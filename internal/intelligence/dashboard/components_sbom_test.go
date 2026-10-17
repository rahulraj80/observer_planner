package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// TestComponentsSBOMDownload pins GET /api/components/sbom (T-670, lane
// MP-REL): a CycloneDX document of the running binary, offered as a file
// download, naming the binary's distro and components digest. It is a read
// (VIEW, SectionNone) like /api/components.
func TestComponentsSBOMDownload(t *testing.T) {
	s := newRemoteTestServer(t, Options{Version: "1.36.0"})
	h := s.guardedHandler("127.0.0.1:8081")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/components/sbom"))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/components/sbom = %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.cyclonedx+json" {
		t.Errorf("content type = %q", ct)
	}
	rep := components.BuildReport(components.BinaryNode, false)
	wantName := `attachment; filename="observer-` + string(rep.Distro) + `-1.36.0.cdx.json"`
	if cd := rec.Header().Get("Content-Disposition"); cd != wantName {
		t.Errorf("content disposition = %q, want %q", cd, wantName)
	}
	var doc struct {
		BOMFormat string `json:"bomFormat"`
		Metadata  struct {
			Properties []struct{ Name, Value string } `json:"properties"`
		} `json:"metadata"`
		Components []struct{ Purl string } `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.BOMFormat != "CycloneDX" || len(doc.Components) == 0 {
		t.Fatalf("not a CycloneDX document with components: %s", sbomTestPrefix(rec.Body.String(), 300))
	}
	props := map[string]string{}
	for _, p := range doc.Metadata.Properties {
		props[p.Name] = p.Value
	}
	if props["superbased:distro"] != string(rep.Distro) || props["superbased:components_digest"] != rep.ComponentsDigest {
		t.Errorf("generation context = %v, want distro %s digest %s", props, rep.Distro, rep.ComponentsDigest)
	}
	if !strings.HasPrefix(doc.Components[0].Purl, "pkg:golang/std@") {
		t.Errorf("first component = %s, want the Go standard library", doc.Components[0].Purl)
	}

	_, caps, sections := s.registerRoutes(nil)
	if caps["/api/components/sbom"] != CapabilityView || sections["/api/components/sbom"] != SectionNone {
		t.Errorf("capability %v section %v, want View and SectionNone", caps["/api/components/sbom"], sections["/api/components/sbom"])
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, loopbackRequest(http.MethodPost, "/api/components/sbom"))
	if rec.Code == http.StatusOK {
		t.Error("POST /api/components/sbom must not succeed")
	}
}

func sbomTestPrefix(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
