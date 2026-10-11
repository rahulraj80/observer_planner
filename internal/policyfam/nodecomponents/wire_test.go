package nodecomponents

import (
	"strings"
	"testing"
)

const testCap = 64 << 10

func TestDecodeBodyStrict(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "empty object", raw: `{}`},
		{name: "plan example shape", raw: `{"distro":"managed","allow":["adapter.codex"],"require":["org.client"],"deny":[{"selector":"remote"},{"selector":"egress.*","grantable":true}],"unlisted":"inert","on_remove":{"teardown":"auto","data":"keep","purge_after_days":0}}`},
		{name: "unknown field", raw: `{"allow":[],"widen":true}`, wantErr: "unknown field"},
		{name: "bare string deny is authoring shorthand, never the signed body", raw: `{"deny":["remote"]}`, wantErr: "cannot unmarshal"},
		{name: "trailing bytes", raw: `{} {}`, wantErr: "trailing bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeBody([]byte(tc.raw), testCap)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
	if _, err := DecodeBody([]byte(`{}`), 1); err == nil {
		t.Error("over-cap body decoded")
	}
}

// TestCanonicalJSONGolden pins the canonical encoding (third-pass N4):
// entries sorted, duplicates removed, grantable omitted when false, the
// unlisted rule and the default on_remove spelled out.
func TestCanonicalJSONGolden(t *testing.T) {
	a := BodyV1{
		Allow:   []string{"terminals", "adapter.codex", "terminals"},
		Require: []string{"org.client"},
		Deny:    []DenyEntryV1{{Selector: "remote"}, {Selector: "egress.*", Grantable: true}, {Selector: "remote"}},
	}
	got, err := CanonicalJSON(a)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"allow":["adapter.codex","terminals"],"require":["org.client"],"deny":[{"selector":"egress.*","grantable":true},{"selector":"remote"}],"unlisted":"inert","on_remove":{"teardown":"auto","data":"keep","purge_after_days":0}}`
	if string(got) != want {
		t.Fatalf("canonical =\n%s\nwant\n%s", got, want)
	}
	// Idempotent: decoding the canonical bytes and re-encoding is a fixed
	// point, so the signed hash never depends on who canonicalised.
	b, err := DecodeBody(got, testCap)
	if err != nil {
		t.Fatal(err)
	}
	again, err := CanonicalJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != want {
		t.Fatalf("canonical form is not a fixed point:\n%s", again)
	}
}

func TestCanonicalizeRejectsContradictoryDeny(t *testing.T) {
	_, err := CanonicalJSON(BodyV1{Deny: []DenyEntryV1{{Selector: "remote"}, {Selector: "remote", Grantable: true}}})
	if err == nil || !strings.Contains(err.Error(), "both grantable and not") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompileBodyStrictAndLenient(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		strictErr   string
		lenientErr  string
		wantIgnored string
	}{
		{name: "valid", raw: `{"allow":["adapter.*","class:integration","stability:stable"],"require":["org.client"]}`},
		{name: "unknown exact id", raw: `{"allow":["adapter.future-tool"]}`, strictErr: "not a component of catalog version", wantIgnored: "allow:adapter.future-tool"},
		{name: "org server id", raw: `{"allow":["org.digest"]}`, strictErr: "org server component", wantIgnored: "allow:org.digest"},
		{name: "bad syntax fails both", raw: `{"allow":["Adapter.Codex"]}`, strictErr: "not a component id", lenientErr: "not a component id"},
		{name: "glob only at the end", raw: `{"allow":["adapter.*.x"]}`, strictErr: "not a component id", lenientErr: "not a component id"},
		{name: "unknown class", raw: `{"allow":["class:analytics"]}`, strictErr: "not a node component class", lenientErr: "not a node component class"},
		{name: "server class", raw: `{"allow":["class:server"]}`, strictErr: "not a node component class", lenientErr: "not a node component class"},
		{name: "unknown stability", raw: `{"allow":["stability:beta"]}`, strictErr: "not a stability level", lenientErr: "not a stability level"},
		{name: "core cannot be denied by id", raw: `{"deny":[{"selector":"core"}]}`, strictErr: "core and cannot be denied", lenientErr: "core and cannot be denied"},
		{name: "core cannot be denied by class", raw: `{"deny":[{"selector":"class:core"}]}`, strictErr: "core cannot be denied", lenientErr: "core cannot be denied"},
		{name: "a broad deny that also matches core is fine", raw: `{"deny":[{"selector":"stability:stable"}]}`},
		{name: "require and deny on one id cannot publish (conflict row 1)", raw: `{"require":["terminals"],"deny":[{"selector":"class:feature"}]}`, strictErr: "matched by require", lenientErr: "matched by require"},
		{name: "unknown distro", raw: `{"distro":"tiny"}`, strictErr: "not a prebuilt node distro", lenientErr: "not a prebuilt node distro"},
		{name: "unknown unlisted rule", raw: `{"unlisted":"active"}`, strictErr: "not supported", lenientErr: "not supported"},
		{name: "purge needs days", raw: `{"on_remove":{"teardown":"auto","data":"purge","purge_after_days":0}}`, strictErr: "0 means never", lenientErr: "0 means never"},
		{name: "days need purge", raw: `{"on_remove":{"teardown":"auto","data":"keep","purge_after_days":30}}`, strictErr: "needs data", lenientErr: "needs data"},
		{name: "scheduled purge", raw: `{"on_remove":{"teardown":"manual","data":"purge","purge_after_days":30}}`},
		{name: "bad teardown", raw: `{"on_remove":{"teardown":"never","data":"keep","purge_after_days":0}}`, strictErr: "teardown", lenientErr: "teardown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := CompileBody([]byte(tc.raw), testCap)
			checkErr(t, "strict", err, tc.strictErr)
			spec, _, err := CompileBodyLenient([]byte(tc.raw), testCap)
			checkErr(t, "lenient", err, tc.lenientErr)
			if tc.wantIgnored != "" && strings.Join(spec.IgnoredIDs, ",") != tc.wantIgnored {
				t.Errorf("ignored = %v, want %s", spec.IgnoredIDs, tc.wantIgnored)
			}
		})
	}
}

func checkErr(t *testing.T, mode string, err error, want string) {
	t.Helper()
	if want == "" && err != nil {
		t.Errorf("%s: unexpected error: %v", mode, err)
	}
	if want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
		t.Errorf("%s: err = %v, want containing %q", mode, err, want)
	}
}
