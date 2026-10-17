package sidecar

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// releasedFileV1 is a FIXTURE: the governance sidecar struct exactly as the
// released observer builds declare it (schema 1, before request-grants), and
// releasedDecode is their strict decoder. Never edit either to make a test
// pass; they stand for binaries already on developer machines, which an
// enterprise update rollback puts back next to a file THIS build wrote.
type releasedFileV1 struct {
	Schema         int             `json:"schema"`
	WriterVersion  string          `json:"writer_version,omitempty"`
	WrittenAt      string          `json:"written_at,omitempty"`
	State          string          `json:"state"`
	OrgKey         string          `json:"org_key,omitempty"`
	Generation     int64           `json:"generation,omitempty"`
	OrgName        string          `json:"org_name,omitempty"`
	FamilyVersion  int64           `json:"family_version,omitempty"`
	EffectiveHash  string          `json:"effective_hash,omitempty"`
	GrantExpiresAt string          `json:"grant_expires_at,omitempty"`
	Pinned         map[string]any  `json:"pinned,omitempty"`
	Share          map[string]any  `json:"share,omitempty"`
	Features       map[string]bool `json:"features,omitempty"`
}

func releasedDecode(raw []byte) (releasedFileV1, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f releasedFileV1
	if err := dec.Decode(&f); err != nil {
		return releasedFileV1{}, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return releasedFileV1{}, errors.New("trailing bytes")
	}
	return f, nil
}

func jsonFieldNames(t *testing.T, v any) []string {
	t.Helper()
	typ := reflect.TypeOf(v)
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			t.Fatalf("%s.%s has no json name", typ.Name(), typ.Field(i).Name)
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func releasedFieldsFixture(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("testdata/released-v1-fields.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Fields []string `json:"fields"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	sort.Strings(fx.Fields)
	return fx.Fields
}

// TestFileShapeIsTheReleasedShape: the sidecar struct this build writes has
// exactly the released field set (the testdata fixture), and the copied
// released struct agrees with the fixture, so neither can drift alone. A new
// field here fails this test on purpose: put the new class in its own file.
func TestFileShapeIsTheReleasedShape(t *testing.T) {
	want := releasedFieldsFixture(t)
	if got := jsonFieldNames(t, releasedFileV1{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("the released-struct fixture drifted from testdata: %v vs %v", got, want)
	}
	if got := jsonFieldNames(t, File{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("sidecar.File json fields = %v, released readers know only %v: a released reader would refuse this build's file whole; put a new directive class in its own sidecar file", got, want)
	}
}

// TestSidecarDecodesWithReleasedStrictDecoder: a file this build writes,
// with every field populated, decodes under the released strict decoder,
// and the pins a released reader then applies are the full set (a feature
// exception never travels in this file, so a rolled-back binary applies
// every pin: the stricter reading). The reverse holds too: a file a
// released build wrote decodes under this build's Decode.
func TestSidecarDecodesWithReleasedStrictDecoder(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	in := File{
		Schema: MaxSchema, WriterVersion: "1.34.0", WrittenAt: FormatTime(now),
		State: StateApplied, OrgKey: "ok", Generation: 7, OrgName: "Acme",
		FamilyVersion: 14, EffectiveHash: "deadbeef",
		GrantExpiresAt: FormatTime(now.Add(24 * time.Hour)),
		Pinned:         map[string]any{"guard.enabled": true, "guard.mode": "enforce"},
		Share:          map[string]any{"full_content": true},
		Features:       map[string]bool{"guard": true},
	}
	// Every field set, so a field added to File without the fixture is
	// encoded here (an omitempty zero would hide it).
	v := reflect.ValueOf(in)
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).IsZero() {
			t.Fatalf("test fixture leaves File.%s zero; populate it", v.Type().Field(i).Name)
		}
	}
	raw, err := Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	old, err := releasedDecode(raw)
	if err != nil {
		t.Fatalf("a released observer refuses the sidecar this build writes: %v\n%s", err, raw)
	}
	if old.EffectiveHash != "deadbeef" || old.Pinned["guard.mode"] != "enforce" || len(old.Pinned) != 2 {
		t.Fatalf("released reader saw %+v", old)
	}

	releasedRaw, err := json.Marshal(releasedFileV1{
		Schema: 1, State: "inert", EffectiveHash: "cafe",
		Pinned: map[string]any{"guard.enabled": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(releasedRaw); err != nil {
		t.Fatalf("this build refuses a released sidecar: %v", err)
	}
}
