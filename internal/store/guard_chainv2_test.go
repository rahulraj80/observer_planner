package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/policy"
)

// v1ChainHash is the chain_hash every pre-G3 row carries, written out here
// independently of the store's own code: SHA-256(prev || 0x1e || the 17
// fields joined by 0x1f). It pins that chains written before chain input v2
// still verify byte for byte.
func v1ChainHash(prev string, fields []string) string {
	h := sha256.New()
	h.Write([]byte(prev))
	h.Write([]byte{0x1e})
	h.Write([]byte(strings.Join(fields, "\x1f")))
	return hex.EncodeToString(h.Sum(nil))
}

// TestGuardChainV1RowsStillVerify: rows a pre-G3 node wrote (chain_v 1, no
// grant_id in the preimage) verify unchanged, and new v2 rows chain on to
// them.
func TestGuardChainV1RowsStillVerify(t *testing.T) {
	t.Parallel()
	s, database := newTestStore(t)
	ctx := context.Background()
	prev := ""
	for i, rule := range []string{"R-101", "R-110"} {
		ts := timestamp(t0Guard.Add(time.Duration(i) * time.Second))
		fields := []string{
			ts, "sA", "", "", "claude-code", "shell_exec", rule, "destructive", "critical",
			"deny", "", "1", "builtin", "old row", "deadbeef", "rm -rf ~/x", "",
		}
		hash := v1ChainHash(prev, fields)
		if _, err := database.ExecContext(ctx, `
			INSERT INTO guard_events (ts, session_id, tool, event_kind, rule_id, category, severity, decision,
			  enforced, source, reason, target_hash, target_excerpt, chain_prev, chain_hash)
			VALUES (?, 'sA', 'claude-code', 'shell_exec', ?, 'destructive', 'critical', 'deny', 1, 'builtin',
			  'old row', 'deadbeef', 'rm -rf ~/x', ?, ?)`, ts, rule, prev, hash); err != nil {
			t.Fatalf("seed v1 row: %v", err)
		}
		prev = hash
	}
	ev := guardEventFixture("sA", "R-171", t0Guard.Add(5*time.Second))
	ev.GrantID = 12
	if _, err := s.InsertGuardEvents(ctx, []GuardEventRow{ev, guardEventFixture("sA", "R-1", t0Guard.Add(6*time.Second))}); err != nil {
		t.Fatal(err)
	}
	var v1, v2 int
	if err := database.QueryRowContext(ctx, `SELECT SUM(chain_v = 1), SUM(chain_v = 2) FROM guard_events`).Scan(&v1, &v2); err != nil {
		t.Fatal(err)
	}
	if v1 != 2 || v2 != 2 {
		t.Fatalf("chain versions v1=%d v2=%d, want 2 and 2", v1, v2)
	}
	report, err := s.VerifyGuardChain(ctx)
	if err != nil || !report.OK || report.Checked != 4 {
		t.Fatalf("mixed chain: %+v %v", report, err)
	}
}

// TestGuardChainV2BindsGrantID: grant_id is in the v2 preimage, so editing,
// adding or removing it after insert is detected, and so is moving a row back
// to chain input v1 to drop the field from the preimage.
func TestGuardChainV2BindsGrantID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		tamper string
	}{
		{"grant id changed", `UPDATE guard_events SET grant_id = 99 WHERE id = 2`},
		{"grant id removed", `UPDATE guard_events SET grant_id = NULL WHERE id = 2`},
		{"grant id added to a plain row", `UPDATE guard_events SET grant_id = 7 WHERE id = 1`},
		{"downgraded to chain input v1", `UPDATE guard_events SET chain_v = 1, grant_id = NULL WHERE id = 2`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, database := newTestStore(t)
			ctx := context.Background()
			plain := guardEventFixture("sA", "R-101", t0Guard)
			granted := guardEventFixture("sA", "R-171", t0Guard.Add(time.Second))
			granted.GrantID = 12
			granted.Decision = "flag"
			granted.DegradedFrom = "org_grant"
			if _, err := s.InsertGuardEvents(ctx, []GuardEventRow{plain, granted}); err != nil {
				t.Fatal(err)
			}
			if r, err := s.VerifyGuardChain(ctx); err != nil || !r.OK {
				t.Fatalf("clean chain: %+v %v", r, err)
			}
			var got int64
			if err := database.QueryRowContext(ctx, `SELECT grant_id FROM guard_events WHERE id = 2`).Scan(&got); err != nil || got != 12 {
				t.Fatalf("grant_id stored = %d (%v)", got, err)
			}
			if _, err := database.ExecContext(ctx, tc.tamper); err != nil {
				t.Fatal(err)
			}
			r, err := s.VerifyGuardChain(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if r.OK || !strings.Contains(r.Detail, "rewritten row") {
				t.Fatalf("tamper not detected: %+v", r)
			}
		})
	}
}

// TestPersistGuardVerdictsCarriesGrantID: the one verdict translation seam
// stamps the grant that covered a call onto its audit row.
func TestPersistGuardVerdictsCarriesGrantID(t *testing.T) {
	t.Parallel()
	s, database := newTestStore(t)
	ctx := context.Background()
	v := guard.ActionVerdict{
		Input:        guard.ActionInput{SessionID: "sA", Tool: "claude-code", Target: "curl -T x", Timestamp: t0Guard},
		Kind:         policy.KindShellExec,
		Verdict:      policy.Verdict{RuleID: "R-171", Decision: policy.DecisionFlag, Severity: policy.SeverityHigh},
		DegradedFrom: "org_grant",
		GrantID:      12,
	}
	if _, err := s.PersistGuardVerdicts(ctx, []guard.ActionVerdict{v}); err != nil {
		t.Fatal(err)
	}
	var grant int64
	var degraded string
	if err := database.QueryRowContext(ctx, `SELECT grant_id, degraded_from FROM guard_events`).Scan(&grant, &degraded); err != nil {
		t.Fatal(err)
	}
	if grant != 12 || degraded != "org_grant" {
		t.Fatalf("row grant_id=%d degraded_from=%q", grant, degraded)
	}
}
