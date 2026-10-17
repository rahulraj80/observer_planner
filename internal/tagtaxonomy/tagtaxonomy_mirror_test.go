package tagtaxonomy

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestTypeScriptMirrorMatches guards the coupling documented in doc.go: the
// ONE TypeScript copy, shared/lib/tagTaxonomy.ts (read by the node
// dashboard's tag picker and the cloud portal's correction form, D-571 /
// D-577), must carry EXACTLY the same slug set as the Go source of truth. A
// drift (a tag added on one side only) would let a picker and the cloud enum
// disagree, so this fails loudly instead.
func TestTypeScriptMirrorMatches(t *testing.T) {
	checkMirror(t, "../../shared/lib/tagTaxonomy.ts")
}

// TestAppCopiesAreShims pins that neither app grows its own copy back: each
// app's lib/tagTaxonomy.ts only re-exports the shared file.
func TestAppCopiesAreShims(t *testing.T) {
	for _, shim := range []string{
		"../../web/src/lib/tagTaxonomy.ts",
		"../../webcloud/src/lib/tagTaxonomy.ts",
	} {
		raw, err := os.ReadFile(shim)
		if err != nil {
			t.Fatalf("read %s: %v", shim, err)
		}
		if !strings.Contains(string(raw), `export * from "@shared/lib/tagTaxonomy";`) {
			t.Errorf("%s is not a re-export shim of shared/lib/tagTaxonomy.ts", shim)
		}
		if regexp.MustCompile(`slug:\s*"`).Match(raw) {
			t.Errorf("%s carries its own tag list; keep the one copy in shared/lib", shim)
		}
	}
}

func checkMirror(t *testing.T, mirror string) {
	t.Helper()
	raw, err := os.ReadFile(mirror)
	if err != nil {
		t.Fatalf("read TS mirror %s: %v", mirror, err)
	}
	// Match the STANDARD_TAGS entries' `slug: "..."`. The category/definition
	// helpers use other keys, so anchoring on `slug:` is precise.
	re := regexp.MustCompile(`slug:\s*"([a-z0-9._-]+)"`)
	found := map[string]struct{}{}
	for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
		found[m[1]] = struct{}{}
	}
	tsSlugs := make([]string, 0, len(found))
	for s := range found {
		tsSlugs = append(tsSlugs, s)
	}
	sort.Strings(tsSlugs)

	goSlugs := Slugs()

	if len(tsSlugs) != len(goSlugs) {
		t.Fatalf("slug count drift: Go has %d, TS has %d\n  Go: %v\n  TS: %v",
			len(goSlugs), len(tsSlugs), goSlugs, tsSlugs)
	}
	for i := range goSlugs {
		if goSlugs[i] != tsSlugs[i] {
			t.Fatalf("slug mismatch at %d: Go=%q TS=%q\n  Go: %v\n  TS: %v",
				i, goSlugs[i], tsSlugs[i], goSlugs, tsSlugs)
		}
	}
}
