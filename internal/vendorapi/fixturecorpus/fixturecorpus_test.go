package fixturecorpus

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

func goodSidecar(fixture []byte) Sidecar {
	return Sidecar{
		SurfaceID: "acme.admin.usage", Scenario: ScenarioNormalPage,
		SourceURL: "https://docs.example.com/admin", SourceKind: SourceAPIReferenceExample,
		CheckedOn: "2026-10-07", SpecVersion: "docs page fetched 2026-10-07",
		SourceSHA256:  strings.Repeat("ab", 32),
		FixtureSHA256: FixtureSHA256(fixture),
		HTTP:          HTTPExchange{Method: "GET", Path: "/v1/usage", Status: 200},
		FieldLedger: map[string]vendorsurface.EvidenceGrade{
			"data[].id": vendorsurface.EvidenceVerifiedOfficial, "data[].api_key\\.created.id": vendorsurface.EvidenceInferred,
			"has_more": vendorsurface.EvidenceVerifiedOfficial,
		},
		Status: vendorsurface.StatusLabel,
	}
}

func lookOK(id vendorsurface.SurfaceID) (vendorsurface.BuildState, bool) {
	switch id {
	case "acme.admin.usage":
		return vendorsurface.BuildBuildable, true
	case "acme.admin.ar":
		return vendorsurface.BuildAwaitingReference, true
	}
	return "", false
}

func TestValidateTable(t *testing.T) {
	fixture := []byte(`{"data":[{"id":"x","api_key.created":{"id":"k"}}],"has_more":false}`)
	if errs := Validate(goodSidecar(fixture), fixture, false, lookOK); len(errs) != 0 {
		t.Fatalf("good sidecar invalid: %v", errs)
	}
	cases := []struct {
		name string
		mut  func(*Sidecar)
		want string
	}{
		{"unknown surface", func(s *Sidecar) { s.SurfaceID = "acme.nope" }, "not a catalog row"},
		{"awaiting reference", func(s *Sidecar) { s.SurfaceID = "acme.admin.ar" }, "fixtures are owed only"},
		{"scenario", func(s *Sidecar) { s.Scenario = "happy" }, "unknown scenario"},
		{"http url", func(s *Sidecar) { s.SourceURL = "http://docs.example.com" }, "public https URL"},
		{"source kind", func(s *Sidecar) { s.SourceKind = "blog" }, "unknown source_kind"},
		{"date", func(s *Sidecar) { s.CheckedOn = "yesterday" }, "checked_on"},
		{"version", func(s *Sidecar) { s.SpecVersion = " " }, "version_or_commit"},
		{"source hash", func(s *Sidecar) { s.SourceSHA256 = "abc" }, "source_sha256"},
		{"fixture hash", func(s *Sidecar) { s.FixtureSHA256 = strings.Repeat("0", 64) }, "does not match"},
		{"real account", func(s *Sidecar) { s.CapturedFromRealAccount = true }, "public sources only"},
		{"status", func(s *Sidecar) { s.Status = "verified" }, "status must be"},
		{"http", func(s *Sidecar) { s.HTTP.Path = "v1/usage" }, "absolute path"},
		{"error scenario on 200", func(s *Sidecar) { s.Scenario = ScenarioError429 }, "does not agree"},
		{"empty ledger", func(s *Sidecar) { s.FieldLedger = nil }, "field_ledger is empty"},
		{"bad grade", func(s *Sidecar) { s.FieldLedger["has_more"] = "Trusted" }, "unknown grade"},
		{"missing path", func(s *Sidecar) { s.FieldLedger["data[].cost"] = vendorsurface.EvidenceInferred }, "not present"},
		{"unescaped dotted key", func(s *Sidecar) {
			s.FieldLedger["data[].api_key.created.id"] = vendorsurface.EvidenceInferred
		}, "not present"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := goodSidecar(fixture)
			tc.mut(&s)
			for _, e := range Validate(s, fixture, false, lookOK) {
				if strings.Contains(e.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("want an error containing %q", tc.want)
		})
	}
}

func TestValidateNDJSON(t *testing.T) {
	fixture := []byte("{\"day\":\"2025-07-01\",\"user\":\"a\"}\n{\"day\":\"2025-07-01\",\"extra\":1}\n")
	s := goodSidecar(fixture)
	s.FieldLedger = map[string]vendorsurface.EvidenceGrade{
		"day":   vendorsurface.EvidenceVerifiedOfficial,
		"extra": vendorsurface.EvidenceInferred,
	}
	if errs := Validate(s, fixture, true, lookOK); len(errs) != 0 {
		t.Fatalf("ndjson fixture invalid: %v", errs)
	}
	if errs := Validate(s, []byte("not json\n"), true, lookOK); len(errs) == 0 {
		t.Fatal("broken ndjson accepted")
	}
}

func TestResolvePaths(t *testing.T) {
	doc, err := decodeDocs([]byte(`[{"a":{"b.c":[{"d":null}]}},{"e":1}]`), false)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path string
		want bool
	}{
		{"$", true},
		{"[]", true},
		{"[].a", true},
		{`[].a.b\.c`, true},
		{`[].a.b\.c[].d`, true},
		{"[].e", true},
		{"[].a.b", false},
		{"[].f", false},
		{"a", false},
	}
	for _, tc := range cases {
		if got := pathPresent(doc, tc.path); got != tc.want {
			t.Errorf("pathPresent(%s) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestRequiredGradesFeedTheCeiling(t *testing.T) {
	led := map[string]vendorsurface.EvidenceGrade{
		"a": vendorsurface.EvidenceVerifiedOfficial,
		"b": vendorsurface.EvidenceInferred,
	}
	if vendorsurface.SyntheticCeiling(RequiredGrades(led)) != vendorsurface.SynthAssumptionTested {
		t.Fatal("an inferred field must cap the synthetic rung at assumption_tested")
	}
}
