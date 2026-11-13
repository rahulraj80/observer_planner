package sandhold

import (
	"context"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// contract_offer_test.go replays SandHold's first-month offer vectors
// (note 44, 2026-11-13): the offer before an account exists
// (signup-offer), where an account stands (offer, offer-none), and the
// card gate's refusal (sandbox-create-card-required).

// TestContractSignupOffer: the node's offer reads into one plain sentence.
func TestContractSignupOffer(t *testing.T) {
	info, err := contractNode(t, loadVector(t, "signup-offer")).Signup(context.Background())
	if err != nil || info == nil || !info.SelfServe || !info.SignupClosed || !info.InviteAccepted || info.Offer == nil {
		t.Fatalf("signup %+v %v", info, err)
	}
	want := "First month free: 30 days at no charge, up to $80 of use. No card needed for your first 5 sandboxes; after that a card is checked with a $1.00 charge, refunded at once."
	if got := info.Offer.Line(); got != want {
		t.Errorf("line %q, want %q", got, want)
	}
}

// TestContractOffer: the account's plan, and the card page is always the
// node's own.
func TestContractOffer(t *testing.T) {
	c := contractNode(t, loadVector(t, "offer"))
	p, err := c.Offer(context.Background())
	if err != nil || p == nil {
		t.Fatalf("offer %+v %v", p, err)
	}
	if !p.InFreeMonth || p.DaysLeft != 28 || p.SkipsLeft != 5 || p.Card != "none" || !p.CardGate || p.FreeUntil == nil || p.Summary == "" {
		t.Errorf("plan %+v", p)
	}
	// The vector's node is node.example; the client is another address,
	// so the page shown is the client's own node.
	if p.AddCardURL != c.URL+"/app/#/billing" {
		t.Errorf("add-card page %q", p.AddCardURL)
	}
	if l := p.CardLine(); !strings.HasPrefix(l, "5 of 5 sandboxes left before a card is needed") {
		t.Errorf("card line %q", l)
	}
	p.CardNeeded, p.SkipsLeft = true, 0
	if l := p.CardLine(); !strings.HasPrefix(l, "Add a card to start more sandboxes: "+c.URL) {
		t.Errorf("card line %q", l)
	}
	p.Card = "checked"
	if l := p.CardLine(); l != "" {
		t.Errorf("checked card line %q", l)
	}
}

// TestContractOfferNone: an org without the offer is nil, not an error.
func TestContractOfferNone(t *testing.T) {
	p, err := contractNode(t, loadVector(t, "offer-none")).Offer(context.Background())
	if err != nil || p != nil {
		t.Fatalf("offer %+v %v", p, err)
	}
}

// TestContractCardRequired: the card gate's refusal reaches the caller
// with its code and the node's words, and is a plan refusal.
func TestContractCardRequired(t *testing.T) {
	v := loadVector(t, "sandbox-create-card-required")
	b := contractBox(t, v)
	_, err := b.Create(context.Background(), sandboxremote.BoxSpec{TimeoutSeconds: 600, Tier: vocab.TierInternet})
	ae, ok := PlanRefusal(err)
	if !ok || ae.Code != CodeCardRequired || ae.Status != 402 || !strings.Contains(ae.Msg, "a card is needed") || !strings.Contains(ae.Fix, "/app/#/billing") {
		t.Fatalf("err %v (%#v)", err, ae)
	}
	if !strings.Contains(err.Error(), "the node refused the sandbox") {
		t.Errorf("err %v", err)
	}
}
