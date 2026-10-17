package agentconfig

import (
	"errors"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
)

func cand(l Layer, m Mode, v any) Candidate {
	return Candidate{Layer: l, Mode: m, Value: v, Source: string(l) + "/" + string(m)}
}

func team(m Mode, t string, v any) Candidate {
	return Candidate{Layer: LayerO2, Mode: m, Value: v, Team: t, Source: "team " + t}
}

// below returns one candidate for every table row ranked below rank (as
// applied with or without a grant), each valued with its own layer name.
func below(rank int, grant bool) []Candidate {
	var out []Candidate
	for _, r := range precedenceTable {
		rr := r.Rank
		if grant {
			rr = r.GrantRank
		}
		if rr < rank {
			out = append(out, cand(r.Layer, r.Mode, string(r.Layer)+"/"+string(r.Mode)))
		}
	}
	return out
}

// TestPrecedenceTableRows is one case per section 3.2 row, with and
// without the developer's grant: the row's value beats every row ranked
// below it, and the row ranked next above beats it.
func TestPrecedenceTableRows(t *testing.T) {
	for _, grant := range []bool{false, true} {
		for _, r := range precedenceTable {
			rank := r.Rank
			if grant {
				rank = r.GrantRank
			}
			own := string(r.Layer) + "/" + string(r.Mode)
			cs := append(below(rank, grant), cand(r.Layer, r.Mode, own))
			res, err := Resolve(ResolveInput{Managed: true, Grant: grant, Candidates: cs})
			if err != nil {
				t.Fatalf("grant=%v rank %d: %v", grant, rank, err)
			}
			if res.Value != own || res.Rank != rank || len(res.Shadowed) != rank-1 {
				t.Errorf("grant=%v rank %d (%s): won by %v at %d with %d shadowed", grant, rank, own, res.Value, res.Rank, len(res.Shadowed))
			}
			for _, above := range precedenceTable {
				ar := above.Rank
				if grant {
					ar = above.GrantRank
				}
				if ar != rank+1 {
					continue
				}
				top := string(above.Layer) + "/" + string(above.Mode)
				res, _ := Resolve(ResolveInput{Managed: true, Grant: grant, Candidates: append(cs, cand(above.Layer, above.Mode, top))})
				if res.Value != top {
					t.Errorf("grant=%v: %s should beat %s, got %v", grant, top, own, res.Value)
				}
			}
		}
	}
	// The ranks are a permutation 1..11 in both columns.
	for _, grant := range []bool{false, true} {
		seen := map[int]bool{}
		for _, r := range precedenceTable {
			rr := r.Rank
			if grant {
				rr = r.GrantRank
			}
			seen[rr] = true
		}
		for i := 1; i <= len(precedenceTable); i++ {
			if !seen[i] {
				t.Errorf("grant=%v: rank %d missing", grant, i)
			}
		}
	}
}

func TestResolveRules(t *testing.T) {
	approval, _ := CommonSpecFor(KeyApprovalMode)
	cases := []struct {
		name    string
		in      ResolveInput
		want    any
		wantErr error
		merged  bool
		ignored int
	}{
		{name: "nothing set", in: ResolveInput{Managed: true}, want: nil},
		{
			name: "D-122 no grant: org default beats the developer's project value",
			in:   ResolveInput{Managed: true, Candidates: []Candidate{cand(LayerL3, ModeNone, "dev"), cand(LayerO1, ModeDefault, "org")}},
			want: "org",
		},
		{
			name: "D-122 grant: the developer's explicit value beats every org default",
			in:   ResolveInput{Managed: true, Grant: true, Candidates: []Candidate{cand(LayerL1, ModeNone, "dev"), cand(LayerO3, ModeDefault, "member")}},
			want: "dev",
		},
		{
			name: "D-122 grant: an org default still fills a gap",
			in:   ResolveInput{Managed: true, Grant: true, Candidates: []Candidate{cand(LayerVendorDefault, ModeNone, "vendor"), cand(LayerO1, ModeDefault, "org")}},
			want: "org",
		},
		{
			name: "D-122 grant: enforced wins grant or not",
			in:   ResolveInput{Managed: true, Grant: true, Candidates: []Candidate{cand(LayerL3, ModeNone, "dev"), cand(LayerO1, ModeEnforced, "org")}},
			want: "org",
		},
		{
			name: "individual node: org layers ignored",
			in:   ResolveInput{Managed: false, Candidates: []Candidate{cand(LayerL2, ModeNone, "dev"), cand(LayerO1, ModeEnforced, "org")}},
			want: "dev", ignored: 1,
		},
		{
			name: "individual node: the vendor's own managed layer still applies",
			in:   ResolveInput{Managed: false, Candidates: []Candidate{cand(LayerL3, ModeNone, "dev"), cand(LayerVendorManaged, ModeNone, "mdm")}},
			want: "mdm",
		},
		{
			name: "ordered: a member enforced value cannot be looser than the org's",
			in: ResolveInput{Managed: true, Order: approval.Order, Enum: approval.Enum, Candidates: []Candidate{
				cand(LayerO1, ModeEnforced, "ask_edits"), cand(LayerO3, ModeEnforced, "full_auto"),
			}},
			want: "ask_edits", merged: true,
		},
		{
			name: "ordered: strictest among defaults too",
			in: ResolveInput{Managed: true, Order: approval.Order, Enum: approval.Enum, Candidates: []Candidate{
				cand(LayerO1, ModeDefault, "plan"), cand(LayerO3, ModeDefault, "auto_edits"), cand(LayerL3, ModeNone, "full_auto"),
			}},
			want: "plan", merged: true,
		},
		{
			name: "ordered: enforced tier beats a stricter default",
			in: ResolveInput{Managed: true, Order: approval.Order, Enum: approval.Enum, Candidates: []Candidate{
				cand(LayerO3, ModeDefault, "plan"), cand(LayerO1, ModeEnforced, "auto_edits"),
			}},
			want: "auto_edits",
		},
		{
			name: "ordered: deny lists union",
			in: ResolveInput{Managed: true, Order: OrderUnion, Candidates: []Candidate{
				cand(LayerO1, ModeEnforced, []any{"shell:rm"}), team(ModeEnforced, "a", []any{"fetch", "shell:rm"}), cand(LayerO3, ModeEnforced, []any{"mcp:x"}),
			}},
			want: []any{"shell:rm", "fetch", "mcp:x"}, merged: true,
		},
		{
			name: "ordered: allow lists intersect",
			in: ResolveInput{Managed: true, Order: OrderIntersect, Candidates: []Candidate{
				cand(LayerO1, ModeEnforced, []any{"read", "shell:git status", "fetch"}), cand(LayerO3, ModeEnforced, []any{"fetch", "read"}),
			}},
			want: []any{"read", "fetch"}, merged: true,
		},
		{
			name: "ordered: telemetry off wins",
			in: ResolveInput{Managed: true, Order: OrderTrueStrict, Candidates: []Candidate{
				cand(LayerO1, ModeEnforced, true), cand(LayerO3, ModeEnforced, false),
			}},
			want: true, merged: true,
		},
		{
			name:    "ordered: a value outside the enum",
			in:      ResolveInput{Managed: true, Order: approval.Order, Enum: approval.Enum, Candidates: []Candidate{cand(LayerO1, ModeEnforced, "yolo")}},
			wantErr: ErrBadValue,
		},
		{
			name:    "D-123: differing team values without a priority",
			in:      ResolveInput{Managed: true, Candidates: []Candidate{team(ModeDefault, "a", "m1"), team(ModeDefault, "b", "m2")}},
			wantErr: ErrTeamConflict,
		},
		{
			name: "D-123: the team priority resolves it",
			in:   ResolveInput{Managed: true, TeamPriority: []string{"b", "a"}, Candidates: []Candidate{team(ModeDefault, "a", "m1"), team(ModeDefault, "b", "m2")}},
			want: "m2",
		},
		{
			name:    "D-123: a priority that omits a conflicting team does not resolve it",
			in:      ResolveInput{Managed: true, TeamPriority: []string{"a"}, Candidates: []Candidate{team(ModeEnforced, "a", "m1"), team(ModeEnforced, "b", "m2")}},
			wantErr: ErrTeamConflict,
		},
		{
			name: "D-123: equal team values are no conflict",
			in:   ResolveInput{Managed: true, Candidates: []Candidate{team(ModeDefault, "a", "same"), team(ModeDefault, "b", "same")}},
			want: "same",
		},
		{
			name: "D-123: ordered-danger team values resolve strictest, no priority needed",
			in: ResolveInput{Managed: true, Order: approval.Order, Enum: approval.Enum, Candidates: []Candidate{
				team(ModeEnforced, "a", "full_auto"), team(ModeEnforced, "b", "ask"),
			}},
			want: "ask", merged: true,
		},
		{
			name:    "two values in the member layer",
			in:      ResolveInput{Managed: true, Candidates: []Candidate{cand(LayerO3, ModeEnforced, "x"), cand(LayerO3, ModeEnforced, "y")}},
			wantErr: ErrLayerConflict,
		},
		{
			name:    "a mode on a developer layer",
			in:      ResolveInput{Managed: true, Candidates: []Candidate{cand(LayerL1, ModeEnforced, "x")}},
			wantErr: ErrBadCandidate,
		},
		{
			name:    "an org layer with no mode",
			in:      ResolveInput{Managed: true, Candidates: []Candidate{cand(LayerO1, ModeNone, "x")}},
			wantErr: ErrBadCandidate,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Resolve(tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == nil {
				if res.Set {
					t.Fatalf("set to %v, want unset", res.Value)
				}
				return
			}
			if !cfgfmt.Equal(normalized(res.Value), normalized(tc.want)) {
				t.Fatalf("value = %v, want %v", res.Value, tc.want)
			}
			if res.Merged != tc.merged || len(res.Ignored) != tc.ignored {
				t.Fatalf("merged=%v ignored=%d, want %v / %d", res.Merged, len(res.Ignored), tc.merged, tc.ignored)
			}
		})
	}
}

func TestLintTeamConflicts(t *testing.T) {
	values := []TeamValue{
		{Team: "red", Mode: ModeEnforced, Value: "m1"},
		{Team: "blue", Mode: ModeEnforced, Value: "m2"},
		{Team: "green", Mode: ModeEnforced, Value: "m1"},
		{Team: "blue", Mode: ModeDefault, Value: "d"},
	}
	members := map[string][]string{
		"ann": {"red", "blue"},   // conflict (enforced)
		"bob": {"red", "green"},  // same value: fine
		"cat": {"blue"},          // one team: fine
		"dan": {"green", "blue"}, // conflict
	}
	got := LintTeamConflicts(OrderNone, nil, values, members)
	if len(got) != 2 || got[0].Member != "ann" || got[1].Member != "dan" || got[0].Mode != ModeEnforced {
		t.Fatalf("conflicts = %+v", got)
	}
	if got := LintTeamConflicts(OrderNone, []string{"blue", "red", "green"}, values, members); len(got) != 0 {
		t.Fatalf("a full priority order resolves every conflict: %+v", got)
	}
	if got := LintTeamConflicts(OrderNone, []string{"red", "blue"}, values, members); len(got) != 1 || got[0].Member != "dan" {
		t.Fatalf("a priority that omits green leaves dan's conflict: %+v", got)
	}
	if got := LintTeamConflicts(OrderEnum, nil, values, members); got != nil {
		t.Fatalf("ordered-danger keys never need a priority: %+v", got)
	}
}

func TestResolveFiles(t *testing.T) {
	w, shadowed, err := ResolveFiles([]FileValue{
		{FileID: "claude-code/user-settings", Value: "u"},
		{FileID: "claude-code/managed-settings", Value: "m"},
		{FileID: "claude-code/project-settings", Value: "p"},
	})
	if err != nil || w.Value != "m" || len(shadowed) != 2 || shadowed[0].Value != "p" {
		t.Fatalf("winner %v shadowed %v err %v", w, shadowed, err)
	}
	w, _, err = ResolveFiles([]FileValue{{FileID: "claude-code/user-settings", Value: "u"}, {FileID: "claude-code/project-local-settings", Value: "l"}})
	if err != nil || w.Value != "l" {
		t.Fatalf("project-local beats user: %v %v", w, err)
	}
	if _, _, err := ResolveFiles([]FileValue{{FileID: "nope/x"}}); !errors.Is(err, ErrBadCandidate) {
		t.Fatalf("unknown file: %v", err)
	}
}
