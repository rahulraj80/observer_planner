package cursor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

func spendLimitFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "cursor", "cursor.admin.user_spend_limit", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestParseUserSpendLimitResult (lane ENTAPI-L14, T-617): the documented
// success and error answers parse to applied and rejected from the fixture
// corpus; any other shape is schema drift (the executor's unknown effect),
// and the free-text message is never read.
func TestParseUserSpendLimitResult(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   []byte
		want   string
	}{
		{"documented success", 200, spendLimitFixture(t, "normal_page.json"), WriteApplied},
		{"documented error", 200, spendLimitFixture(t, "enum_sweep.json"), WriteRejected},
		{"a 204 with the success body", 204, []byte(`{"outcome":"success"}`), WriteApplied},
		{"an unknown outcome", 200, []byte(`{"outcome":"queued"}`), ""},
		{"no outcome", 200, []byte(`{"message":"ok"}`), ""},
		{"not JSON", 200, []byte(`ok`), ""},
		{"a 401 body", 401, spendLimitFixture(t, "error_401.json"), ""},
	}
	for _, tc := range cases {
		got, err := ParseUserSpendLimitResult(vendorapi.Response{Status: tc.status, Body: tc.body})
		if tc.want == "" {
			if !errors.Is(err, vendorapi.ErrSchemaDrift) {
				t.Errorf("%s: want drift, got %q %v", tc.name, got, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: %q %v", tc.name, got, err)
		}
	}
}
