package sbom

import (
	"bytes"
	"encoding/json"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func sampleInfo() *debug.BuildInfo {
	return &debug.BuildInfo{
		GoVersion: "go1.25.13",
		Path:      "github.com/marmutapp/superbased-observer/cmd/observer",
		Main:      debug.Module{Path: "github.com/marmutapp/superbased-observer", Version: "v1.36.0"},
		Deps: []*debug.Module{
			{Path: "modernc.org/sqlite", Version: "v1.40.0", Sum: "h1:abc="},
			{Path: "github.com/spf13/cobra", Version: "v1.9.1", Sum: "h1:def="},
			{Path: "example.com/old", Version: "v0.1.0", Replace: &debug.Module{Path: "example.com/new", Version: "v0.2.0", Sum: "h1:ghi="}},
		},
		Settings: []debug.BuildSetting{
			{Key: "-tags", Value: "sbo_omit_obs,sbo_distro_developer"},
			{Key: "CGO_ENABLED", Value: "0"},
			{Key: "GOOS", Value: "linux"},
			{Key: "GOARCH", Value: "amd64"},
		},
	}
}

// TestRenderIsCycloneDXAndDeterministic pins the document shape the release
// pipeline attaches per artifact and the Components page downloads: the
// generation context names the distro, tags and catalog version, every
// linked module (replacements resolved) appears with a purl, and the same
// input renders the same bytes.
func TestRenderIsCycloneDXAndDeterministic(t *testing.T) {
	m := Meta{Distro: "developer", CatalogVersion: 1, ComponentsDigest: "d1", BinarySHA256: "aa"}
	a, err := Render(sampleInfo(), m)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Render(sampleInfo(), m)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two renders of the same binary differ")
	}
	var doc map[string]any
	if err := json.Unmarshal(a, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["bomFormat"] != "CycloneDX" || doc["specVersion"] != SpecVersion {
		t.Fatalf("not a CycloneDX %s document: %v %v", SpecVersion, doc["bomFormat"], doc["specVersion"])
	}
	if !strings.HasPrefix(doc["serialNumber"].(string), "urn:uuid:") {
		t.Errorf("serial = %v", doc["serialNumber"])
	}
	md := doc["metadata"].(map[string]any)
	if _, ok := md["timestamp"]; ok {
		t.Error("a zero Timestamp must omit the timestamp (reproducible release SBOMs)")
	}
	props := map[string]string{}
	for _, p := range md["properties"].([]any) {
		pm := p.(map[string]any)
		props[pm["name"].(string)] = pm["value"].(string)
	}
	for k, want := range map[string]string{
		"superbased:distro":            "developer",
		"superbased:build_tags":        "sbo_distro_developer,sbo_omit_obs",
		"superbased:catalog_version":   "1",
		"superbased:components_digest": "d1",
		"superbased:goos":              "linux",
	} {
		if props[k] != want {
			t.Errorf("%s = %q, want %q", k, props[k], want)
		}
	}
	text := string(a)
	for _, want := range []string{"pkg:golang/modernc.org/sqlite@v1.40.0", "pkg:golang/example.com/new@v0.2.0", "pkg:golang/std@1.25.13", `"SHA-256"`} {
		if !strings.Contains(text, want) {
			t.Errorf("document lacks %s", want)
		}
	}
	if strings.Contains(text, "example.com/old") {
		t.Error("a replaced module must be reported as its replacement")
	}
	m2 := m
	m2.Timestamp = time.Unix(1700000000, 0)
	c, err := Render(sampleInfo(), m2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(c), `"timestamp": "2023-11-14T22:13:20Z"`) {
		t.Errorf("timestamp missing: %s", c)
	}
	if _, err := Render(nil, m); err == nil {
		t.Error("nil build info must be an error")
	}
}

func TestTagsAndModules(t *testing.T) {
	bi := sampleInfo()
	if got := strings.Join(Tags(bi), ","); got != "sbo_distro_developer,sbo_omit_obs" {
		t.Errorf("tags = %s", got)
	}
	if got := Tags(&debug.BuildInfo{}); len(got) != 0 {
		t.Errorf("no -tags setting must be no tags, got %v", got)
	}
	if got := strings.Join(Modules(bi), " "); got != "example.com/new@v0.2.0 github.com/spf13/cobra@v1.9.1 modernc.org/sqlite@v1.40.0" {
		t.Errorf("modules = %s", got)
	}
}
