package components

import (
	"encoding/base64"
	"strings"
	"testing"
)

// omittable returns a non-core, non-mandatory component of the base that
// can leave it without breaking any requires or bundles, preferring one
// that claims l1 when wantL1 is true. The test finds it from the catalog
// so it never pins today's catalog rows.
func omittable(t *testing.T, base Distro, wantL1 bool) string {
	t.Helper()
	for _, id := range InDistro(base) {
		c, _ := Get(id)
		if c.Class == ClassCore || c.Levels.Has(L1) != wantL1 {
			continue
		}
		set := toSet(InDistro(base))
		delete(set, id)
		if len(ValidateSelection(BinaryNode, sortedKeys(set))) == 0 {
			return id
		}
	}
	t.Skipf("no omittable %s component with L1=%t", base, wantL1)
	return ""
}

func TestResolveManifestValid(t *testing.T) {
	l1 := omittable(t, DistroDeveloper, true)
	m := Manifest{Schema: ManifestSchemaV1, Name: "acme lean", Base: DistroDeveloper, Omit: []string{" " + l1 + " "}}
	cb, problems := ResolveManifest(m)
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	if !IsCustomDistro(cb.Distro) || cb.Distro != CustomDistroPrefix+cb.ManifestDigest {
		t.Fatalf("distro %q digest %q", cb.Distro, cb.ManifestDigest)
	}
	if DistroOf(cb.Compiled) != DistroCustom {
		t.Fatalf("a resolved manifest must not equal a prebuilt distro")
	}
	if cb.ComponentsDigest != Digest(cb.Compiled) {
		t.Fatal("components digest mismatch")
	}
	if !contains(cb.AbsentL1, l1) || contains(cb.Compiled, l1) {
		t.Fatalf("%s should be compiled out (L1): absent %v", l1, cb.AbsentL1)
	}
	want := BuildTagFor(l1)
	if !contains(cb.Tags, want) {
		t.Fatalf("tags %v lack %s", cb.Tags, want)
	}
	// Every developer omit tag is still there: the base excluded those too.
	for _, tag := range OmitTags(DistroDeveloper) {
		if !contains(cb.Tags, tag) {
			t.Errorf("tags lack the base's %s", tag)
		}
	}
	if !contains(cb.DefaultSelection, "core") || contains(cb.DefaultSelection, l1) {
		t.Fatalf("default selection %v", cb.DefaultSelection)
	}
	// The name is a label: it never changes the id.
	m2 := m
	m2.Name = "other"
	if ManifestDigest(m2) != cb.ManifestDigest {
		t.Fatal("the name changed the manifest digest")
	}
	// Order and whitespace never change it either.
	if ManifestDigest(Manifest{Schema: ManifestSchemaV1, Base: DistroDeveloper, Omit: []string{l1}}) != cb.ManifestDigest {
		t.Fatal("normalization changed the manifest digest")
	}
	// The stamp round-trips to the same build, name included.
	back, err := DecodeCustomStamp(cb.Stamp)
	if err != nil {
		t.Fatal(err)
	}
	if back.Distro != cb.Distro || back.Manifest.Name != "acme lean" || back.ComponentsDigest != cb.ComponentsDigest {
		t.Fatalf("stamp round trip: %+v", back)
	}
	if !strings.HasPrefix(cb.LDFlag(), "-X "+CustomStampVar+"=") {
		t.Fatalf("ldflag %q", cb.LDFlag())
	}
}

// TestResolveManifestRules has one case per row of manifestRules.
func TestResolveManifestRules(t *testing.T) {
	l1 := omittable(t, DistroDeveloper, true)
	outOfDev := ""
	for _, c := range ForBinary(BinaryNode) {
		if c.Class != ClassCore && !c.Distros[DistroDeveloper].Compiled() && c.Distros[DistroFull].Compiled() {
			outOfDev = c.ID
			break
		}
	}
	cases := []struct {
		name string
		m    Manifest
		want string
	}{
		{"schema", Manifest{Schema: "sbo-distro.v0", Base: DistroDeveloper, Omit: []string{l1}}, `schema "sbo-distro.v0"`},
		{"name", Manifest{Schema: ManifestSchemaV1, Name: "bad/name", Base: DistroDeveloper, Omit: []string{l1}}, `name "bad/name"`},
		{"base", Manifest{Schema: ManifestSchemaV1, Base: "custom", Omit: []string{l1}}, `base "custom"`},
		{"unknown id", Manifest{Schema: ManifestSchemaV1, Base: DistroDeveloper, Omit: []string{"nope"}}, `omit "nope": not a component`},
		{"core", Manifest{Schema: ManifestSchemaV1, Base: DistroDeveloper, Omit: []string{"core"}}, `core is in every build`},
		{"both", Manifest{Schema: ManifestSchemaV1, Base: DistroDeveloper, Add: []string{l1}, Omit: []string{l1}}, `in both add and omit`},
		{"twice", Manifest{Schema: ManifestSchemaV1, Base: DistroDeveloper, Omit: []string{l1, l1}}, `listed twice`},
		{"redundant add", Manifest{Schema: ManifestSchemaV1, Base: DistroDeveloper, Add: []string{l1}}, `already compiles it`},
		{"mandatory", Manifest{Schema: ManifestSchemaV1, Base: DistroDeveloper, Omit: []string{"org.client"}}, `it can never be omitted`},
		{"prebuilt", Manifest{Schema: ManifestSchemaV1, Base: DistroDeveloper}, `exactly the prebuilt developer distro`},
	}
	if outOfDev != "" {
		cases = append(cases, struct {
			name string
			m    Manifest
			want string
		}{"omit not in base", Manifest{Schema: ManifestSchemaV1, Base: DistroDeveloper, Omit: []string{outOfDev}}, `base does not compile it`})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, problems := ResolveManifest(tc.m)
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Fatalf("problems %v, want one containing %q", problems, tc.want)
			}
		})
	}
}

// TestResolveManifestNamesAMissingRequirement pins the closure rule: an
// omit that leaves a dependant behind is refused and the fix is named.
func TestResolveManifestNamesAMissingRequirement(t *testing.T) {
	in := toSet(InDistro(DistroDeveloper))
	for _, c := range ForBinary(BinaryNode) {
		if !in[c.ID] {
			continue
		}
		for _, r := range c.Requires {
			rc, _ := Get(r)
			if rc.Class == ClassCore || contains(mandatoryNodeComponents, r) || !in[r] {
				continue
			}
			_, problems := ResolveManifest(Manifest{Schema: ManifestSchemaV1, Base: DistroDeveloper, Omit: []string{r}})
			all := strings.Join(problems, "\n")
			if !strings.Contains(all, "needs "+r) || !strings.Contains(all, "add it, or omit the component that needs it") {
				t.Fatalf("omitting %s (required by %s): %v", r, c.ID, problems)
			}
			return
		}
	}
	t.Skip("no developer component requires another non-core developer component")
}

func TestDecodeManifestJSONRefusesUnknownFields(t *testing.T) {
	if _, err := DecodeManifestJSON([]byte(`{"schema":"sbo-distro.v1","base":"developer","extra":1}`)); err == nil {
		t.Fatal("an unknown field was accepted")
	}
	if _, err := DecodeManifestJSON([]byte(`{"schema":"sbo-distro.v1","base":"developer"} {}`)); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
	if _, err := DecodeCustomStamp("!!"); err == nil {
		t.Fatal("a bad stamp encoding was accepted")
	}
	bad := base64.RawURLEncoding.EncodeToString([]byte(`{"schema":"sbo-distro.v1","base":"developer","add":[],"omit":[]}`))
	if _, err := DecodeCustomStamp(bad); err == nil {
		t.Fatal("a stamp resolving to a prebuilt distro was accepted")
	}
}

// TestCustomBuildOffersOnlyItsManifest stamps this process as a custom
// build and checks every surface the stamp feeds: Compiled, DistroOut,
// DistroOf and the report's custom block. A broken stamp offers only the
// mandatory components (fail closed).
func TestCustomBuildOffersOnlyItsManifest(t *testing.T) {
	l1 := omittable(t, DistroDeveloper, true)
	cb, problems := ResolveManifest(Manifest{Schema: ManifestSchemaV1, Base: DistroDeveloper, Omit: []string{l1}})
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	t.Cleanup(func() { resetCustomForTest("") })

	resetCustomForTest(cb.Stamp)
	if got := strings.Join(Compiled(BinaryNode), ","); got != strings.Join(cb.Compiled, ",") {
		t.Fatalf("Compiled = %s\nwant %s", got, strings.Join(cb.Compiled, ","))
	}
	rep := BuildReport(BinaryNode, false)
	if rep.Distro != DistroCustom || rep.ComponentsDigest != cb.ComponentsDigest {
		t.Fatalf("report distro %s digest %s", rep.Distro, rep.ComponentsDigest)
	}
	if rep.Custom == nil || rep.Custom.Distro != cb.Distro || rep.Custom.Error != "" {
		t.Fatalf("report custom block %+v", rep.Custom)
	}
	// This test binary carries every package, so every excluded component
	// is reported under distro_out (the proof gate is what fails a real
	// build that still links an L1 one).
	if !contains(rep.DistroOut, l1) {
		t.Fatalf("distro_out %v lacks %s", rep.DistroOut, l1)
	}

	resetCustomForTest("not-base64!")
	if got := strings.Join(Compiled(BinaryNode), ","); got != strings.Join(mandatoryNodeComponents, ",") {
		t.Fatalf("a broken stamp offers %s, want only the mandatory components", got)
	}
	if rep := BuildReport(BinaryNode, false); rep.Custom == nil || rep.Custom.Error == "" {
		t.Fatalf("a broken stamp must be reported: %+v", rep.Custom)
	}

	resetCustomForTest("")
	if _, stamped, _ := BuildCustom(); stamped {
		t.Fatal("an unstamped build reports a custom stamp")
	}
	if rep := BuildReport(BinaryNode, false); rep.Custom != nil {
		t.Fatal("an unstamped build reports a custom block")
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
