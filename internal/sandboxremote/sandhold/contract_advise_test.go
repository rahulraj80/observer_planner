package sandhold

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The advisor (GET and POST /v1/advise) against SandHold's contract files:
// the three questions with their choices, the advice for the contract's
// answers, and a refusal in the node's words.

func TestContractAdviseQuestions(t *testing.T) {
	c := contractNode(t, loadVector(t, "advise-questions"))
	qs, err := c.AdviseQuestions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, q := range qs {
		ids = append(ids, q.ID)
		if q.Ask == "" || len(q.Choices) == 0 {
			t.Errorf("question %q lacks its words or its choices: %+v", q.ID, q)
		}
		for _, ch := range q.Choices {
			if ch.ID == "" || ch.Label == "" {
				t.Errorf("question %q has a choice without an id or a label: %+v", q.ID, ch)
			}
		}
	}
	if strings.Join(ids, ",") != "runs,trust,reach" {
		t.Errorf("questions %v, want runs, trust and reach in the node's order", ids)
	}
	for _, ch := range qs[2].Choices {
		if ch.ID == "npm" && (len(ch.Hosts) != 1 || ch.Hosts[0] != "registry.npmjs.org") {
			t.Errorf("the npm reach choice was not read with its hosts: %+v", ch)
		}
	}
}

func TestContractAdvise(t *testing.T) {
	c := contractNode(t, loadVector(t, "advise"))
	a, err := c.Advise(context.Background(), AdviseRequest{Runs: "tests", Trust: "generated", Reach: AdviseReach{Packages: []string{"pypi"}, Hosts: []string{"github.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Isolation != "shielded" || a.Network != "limited" || a.IsolationMeans == "" || a.RunsOn != "runc" {
		t.Errorf("the advice was not read as the contract gives it: %+v", a)
	}
	if !strings.HasPrefix(a.Command, "sandhold create --isolation shielded") {
		t.Errorf("command %q, want the node's own command", a.Command)
	}
	if len(a.Why) != 3 || a.Why[0].About != "isolation" || a.Why[0].Because == "" || a.Why[0].Choice == "" {
		t.Errorf("the reasons were not read: %+v", a.Why)
	}
	if len(a.Limits) == 0 || len(a.Notes) == 0 {
		t.Errorf("limits %v and notes %v, want the node's", a.Limits, a.Notes)
	}
}

func TestContractAdviseRefused(t *testing.T) {
	c := contractNode(t, loadVector(t, "advise-refused"))
	_, err := c.Advise(context.Background(), AdviseRequest{Trust: "maybe"})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("got %v, want the node's 400 refusal", err)
	}
	if !strings.Contains(ae.Msg, `trust "maybe" is not one of the answers`) || !strings.Contains(ae.Fix, "own, generated or unknown") {
		t.Errorf("the refusal is not in the node's words: %q / %q", ae.Msg, ae.Fix)
	}
}
