package vendorcoverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/vendorsurface"
	"github.com/marmutapp/superbased-observer/internal/vendorsurface/catalog"
)

var extraForbiddenImports []string

func load(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	return c
}

// TestEveryRegistryRowHasEnterpriseAnswer is the plan section 13 pin: every
// advertised registry row lists catalog surfaces, inherits from a parent, or
// carries an honest None with the date and sources; every cited id resolves.
func TestEveryRegistryRowHasEnterpriseAnswer(t *testing.T) {
	for _, err := range Check(load(t)) {
		t.Error(err)
	}
	for _, tool := range integration.Tools() {
		c, _ := integration.For(tool)
		if c.Lifecycle.Advertised() && !c.Enterprise.Answered() {
			t.Errorf("%s: no enterprise answer", tool)
		}
	}
}

func TestInheritanceResolvesInOnePlace(t *testing.T) {
	cases := []struct{ child, parent string }{{"kiro-crew", "kiro-cli"}, {"grokbot", "cursor"}}
	for _, tc := range cases {
		c, _ := integration.For(tc.child)
		if c.Enterprise.InheritsFrom != tc.parent {
			t.Errorf("%s inherits from %q, want %q", tc.child, c.Enterprise.InheritsFrom, tc.parent)
			continue
		}
		got, ok := SurfacesFor(tc.child)
		want, ok2 := SurfacesFor(tc.parent)
		if !ok || !ok2 {
			t.Errorf("%s or %s does not resolve", tc.child, tc.parent)
			continue
		}
		have := map[vendorsurface.SurfaceID]bool{}
		for _, id := range got {
			have[id] = true
		}
		for _, id := range want {
			if !have[id] {
				t.Errorf("%s misses inherited %s from %s", tc.child, id, tc.parent)
			}
		}
	}
	if _, ok := SurfacesFor("no-such-tool"); ok {
		t.Error("unknown tool resolved")
	}
}

func TestNoneAnswersAreHonest(t *testing.T) {
	c, _ := integration.For("copilot-web")
	if c.Enterprise.None == nil {
		t.Fatal("copilot-web has no documented admin surface (Appendix F); want a None answer")
	}
	if len(c.Enterprise.None.SourcesSearched) == 0 || !vendorsurface.ValidDate(c.Enterprise.None.Checked) {
		t.Fatal("None answer needs the date checked and the sources searched")
	}
	if _, ok := SurfacesFor("copilot-web"); ok {
		t.Fatal("a None answer must not resolve to surfaces")
	}
}

func TestReverseIndex(t *testing.T) {
	rev := ReverseIndex()
	tools := rev["cursor.admin.audit_logs"]
	if strings.Join(tools, ",") != "cursor,grokbot" {
		t.Fatalf("cursor.admin.audit_logs covers %v, want cursor and grokbot (inherited)", tools)
	}
	for id, ts := range rev {
		if len(ts) == 0 {
			t.Errorf("%s indexed with no tools", id)
		}
	}
}

// TestTargetsAreTyped pins plan 4.5 Targets and routes: products, gateway
// routes and conditional providers are separate typed targets, never native
// adapter rows.
func TestTargetsAreTyped(t *testing.T) {
	want := map[string]vendorsurface.TargetKind{
		"m365-copilot":                         vendorsurface.TargetVendorProduct,
		"via-openrouter":                       vendorsurface.TargetGatewayRoute,
		"open-interpreter-via-openai-platform": vendorsurface.TargetConditionalProvider,
		"claude-managed-agents":                vendorsurface.TargetVendorProduct,
		"perplexity-comet":                     vendorsurface.TargetVendorProduct,
	}
	got := map[string]vendorsurface.TargetKind{}
	for _, tg := range Targets() {
		got[tg.Name] = tg.Kind
		if len(tg.Surfaces) == 0 {
			t.Errorf("target %s has no surfaces", tg.Name)
		}
	}
	for name, k := range want {
		if got[name] != k {
			t.Errorf("target %s kind %q, want %q", name, got[name], k)
		}
	}
	// open-interpreter is both a native registry row and a conditional
	// provider target: its native answer must NOT include OpenAI Platform rows
	// until exclusive routing is proven (R-A-15).
	ids, _ := SurfacesFor("open-interpreter")
	for _, id := range ids {
		if strings.HasPrefix(string(id), "openai.platform.") {
			t.Errorf("open-interpreter native answer cites %s; OpenAI Platform is a conditional provider only", id)
		}
	}
}

// TestEnterpriseRelationOnlyInRegistry pins invariant I2: the tool to surface
// relation is authored only in internal/integration. No other Go file may
// declare a tool-keyed surface table, and catalog rows carry no tool field
// (strict decoding rejects one, see catalog.TestDecodeRejectsUnknownFieldsAndWrongGroup).
func TestEnterpriseRelationOnlyInRegistry(t *testing.T) {
	root := ".." // internal/
	needles := []string{
		"map[string]EnterpriseAnswer", "map[string]integration.EnterpriseAnswer",
		"map[string][]vendorsurface.SurfaceID",
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		slash := filepath.ToSlash(path)
		if strings.Contains(slash, "/integration/") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, n := range needles {
			if strings.Contains(string(b), n) {
				t.Errorf("%s declares %s: the tool to surface relation belongs only in internal/integration", slash, n)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
