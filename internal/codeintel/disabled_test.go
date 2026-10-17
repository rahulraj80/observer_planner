package codeintel

import (
	"context"
	"testing"
)

// TestDisabledProvider pins the D-307 contract: the provider for an install
// with code intelligence off answers exactly like Unavailable (fail open,
// nothing available, empty results) and is the ONLY one IsDisabled reports.
func TestDisabledProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name         string
		p            Provider
		wantDisabled bool
	}{
		{name: "disabled", p: Disabled(), wantDisabled: true},
		{name: "unavailable", p: Unavailable(), wantDisabled: false},
		{name: "nil", p: nil, wantDisabled: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsDisabled(tc.p); got != tc.wantDisabled {
				t.Fatalf("IsDisabled = %v, want %v", got, tc.wantDisabled)
			}
			if tc.p == nil {
				return
			}
			if tc.p.Available() {
				t.Error("Available() = true, want false")
			}
			if tc.p.Stale("/repo/a.go") {
				t.Error("Stale() = true, want false")
			}
			syms, err := tc.p.SymbolsInFile(ctx, "/repo/a.go")
			if err != nil || len(syms) != 0 {
				t.Errorf("SymbolsInFile = %v, %v; want empty, nil", syms, err)
			}
			if _, found, err := tc.p.ArchivedProject(ctx, "/repo"); found || err != nil {
				t.Errorf("ArchivedProject found=%v err=%v; want false, nil", found, err)
			}
		})
	}
}
