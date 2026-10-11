package orgclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/policyfam/nodegov"
)

const policyfamNodeGovernance = "node.governance"

// TestFetchAndAcceptPolicyResource_NodeGovernanceVocabSkew pins the node side
// of the section-vocabulary skew fix end to end:
//
//  1. a body the server projected for this node's older vocabulary applies;
//  2. after an upgrade the SAME version arrives with the ids the node used to
//     lack; that is accepted, not refused as a replay;
//  3. a genuinely different body at that version is still a replay;
//  4. a body naming a page this build does not have applies with that one id
//     dropped, instead of failing the whole body (decode_failed).
func TestFetchAndAcceptPolicyResource_NodeGovernanceVocabSkew(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	ps := newPolicyResourceServer(t)
	c, s, cacheDir := enrolledPRClient(t, ps.srv.URL)
	opts := PolicyResourceOptions{
		CacheDir:            cacheDir,
		AcceptFamilies:      []string{policyfamNodeGovernance},
		PreauthorizeEnforce: []string{policyfamNodeGovernance},
	}
	ctx := context.Background()
	fetch := func(step string) PolicyResourceResult {
		t.Helper()
		res, err := c.FetchAndAcceptPolicyResource(ctx, policyfamNodeGovernance, opts)
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		return res
	}

	// 1. Served projected (agent-config stripped for an older vocabulary).
	projected := signedTestResource(t, priv, pub, policyfamNodeGovernance, 7, `{"schema":1,"sections":{"hidden":["cost"]}}`, nil)
	ps.resources[policyfamNodeGovernance] = &projected
	if res := fetch("projected"); res.Status != PRApplied {
		t.Fatalf("projected body: %+v, want applied", res)
	}

	// 2. Same version, the full body: the node now advertises the newer
	// vocabulary and the server stops projecting.
	full := signedTestResource(t, priv, pub, policyfamNodeGovernance, 7, `{"schema":1,"sections":{"hidden":["agent-config","cost"]}}`, nil)
	ps.resources[policyfamNodeGovernance] = &full
	res := fetch("full")
	if res.Status != PRApplied || res.BodyHash != full.BodyHash {
		t.Fatalf("full body at the same version: %+v, want applied (a vocabulary upgrade is not a replay)", res)
	}
	enr, _ := s.LoadEnrolment(ctx)
	st, ok, err := s.LoadPolicyResourceState(ctx, OrgKey(enr.OrgServerURL, enr.OrgID), policyfamNodeGovernance)
	if err != nil || !ok || st.BodyHash != full.BodyHash {
		t.Fatalf("durable state = %+v ok=%v err=%v, want the full body's hash", st, ok, err)
	}
	spec, isGov := res.Spec.(nodegov.PolicySpec)
	if !isGov || strings.Join(spec.HiddenSections, ",") != "agent-config,cost" {
		t.Fatalf("applied spec = %+v", res.Spec)
	}

	// 3. A different policy at the same version is still a replay.
	other := signedTestResource(t, priv, pub, policyfamNodeGovernance, 7, `{"schema":1,"sections":{"hidden":["routing"]}}`, nil)
	ps.resources[policyfamNodeGovernance] = &other
	if res := fetch("other"); res.Status != PRRejected || res.RejectCode != PRRejectVersionReplay {
		t.Fatalf("different body at the same version: %+v, want rejected/version_replay", res)
	}
	// And so is going BACK to the projection once the full body is held: the
	// exception only ever lets the held body grow toward the full one.
	ps.resources[policyfamNodeGovernance] = &projected
	if res := fetch("back to projected"); res.Status != PRRejected || res.RejectCode != PRRejectVersionReplay {
		t.Fatalf("projected body after the full one: %+v, want rejected/version_replay", res)
	}

	// 4. A newer server names a page this build has never heard of.
	future := signedTestResource(t, priv, pub, policyfamNodeGovernance, 8,
		`{"schema":1,"sections":{"hidden":["cost","page-from-the-future"],"read_only":["routing"]}}`, nil)
	ps.resources[policyfamNodeGovernance] = &future
	res = fetch("future")
	if res.Status != PRApplied {
		t.Fatalf("body with an unknown section id: %+v, want applied with the id ignored", res)
	}
	spec, _ = res.Spec.(nodegov.PolicySpec)
	if strings.Join(spec.HiddenSections, ",") != "cost" || strings.Join(spec.ReadOnlySections, ",") != "routing" ||
		strings.Join(spec.IgnoredIDs, ",") != "sections.hidden:page-from-the-future" {
		t.Fatalf("applied spec = %+v", spec)
	}
}
