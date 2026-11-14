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
	o := info.Offer
	if o.BillingOff || o.TopUpUSD != 20 || o.TopUpBelowUSD != 5 || o.FreeLimits == nil || o.FreeLimits.MaxRunMinutes != 30 || o.FreeLimits.AtOnce != 2 || o.FreeLimits.PerDay != 20 ||
		!strings.HasPrefix(o.Expect, "The $1.00 card check is refunded at once.") || !strings.Contains(o.Expect, "charged $20 each time your balance falls below $5") {
		t.Errorf("offer %+v %+v", o, o.FreeLimits)
	}
}

// TestContractSignupOfferBillingOff: a node with billing off offers free
// sandboxes in all, no free month, and says so.
func TestContractSignupOfferBillingOff(t *testing.T) {
	info, err := contractNode(t, loadVector(t, "signup-offer-billing-off")).Signup(context.Background())
	if err != nil || info == nil || info.Offer == nil {
		t.Fatalf("signup %+v %v", info, err)
	}
	o := info.Offer
	if !o.BillingOff || o.FreeSandboxes != 5 || o.FreeDays != 0 || o.FreeLimits == nil || o.FreeLimits.Total != 5 || o.FreeLimits.Kind != "billing_off" {
		t.Fatalf("offer %+v %+v", o, o.FreeLimits)
	}
	want := "Billing is off on this node: 5 free sandboxes in all (each up to 15 min, 1 vCPU, 2 GiB memory, 1 at a time), no card taken."
	if got := o.Line(); got != want {
		t.Errorf("line %q, want %q", got, want)
	}
	if !strings.HasPrefix(o.Expect, "Billing is off on this node, so no card is taken") {
		t.Errorf("expect %q", o.Expect)
	}
}

// TestContractOfferBillingOff: the plan on a node with billing off: no card
// line, and the expect line says what the free sandboxes are.
func TestContractOfferBillingOff(t *testing.T) {
	p, err := contractNode(t, loadVector(t, "offer-billing-off")).Offer(context.Background())
	if err != nil || p == nil {
		t.Fatalf("offer %+v %v", p, err)
	}
	if !p.BillingOff || p.FreeSandboxes != 5 || p.FreeSandboxesLeft != 0 || !p.FreeUse || p.CardLine() != "" {
		t.Errorf("plan %+v", p)
	}
	if l := p.ExpectLine(); !strings.Contains(l, "5 free sandboxes in all") {
		t.Errorf("expect line %q", l)
	}
}

// TestContractPrepaidRefusals: the balance and free-sandbox refusals are
// plan refusals with the node's words.
func TestContractPrepaidRefusals(t *testing.T) {
	for name, code := range map[string]string{"sandbox-create-balance-empty": CodeBalanceEmpty, "sandbox-create-free-used": CodeFreeSandboxesUsed} {
		v := loadVector(t, name)
		_, err := contractBox(t, v).Create(context.Background(), sandboxremote.BoxSpec{TimeoutSeconds: 600, Tier: vocab.TierInternet})
		ae, ok := PlanRefusal(err)
		if !ok || ae.Code != code || ae.Msg == "" || ae.Fix == "" {
			t.Errorf("%s: err %v (%#v)", name, err, ae)
		}
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
	if l := p.LimitsLine(); l != "Free use: each sandbox up to 30 min, 2 vCPU, 4 GiB memory, 10 GiB disk, 2 at a time, 20 a day." {
		t.Errorf("limits line %q", l)
	}
	// A node whose summary already names the sandboxes left must not have
	// its counts repeated by the card line (the CLI printed both).
	p.Summary = "First month free: 30 days left, up to $80 of use; 5 of 5 sandboxes left without a card."
	if l := p.CardLine(); !strings.HasPrefix(l, "Add a card any time at "+c.URL) || strings.Contains(l, "sandboxes left") {
		t.Errorf("card line repeats the summary: %q", l)
	}
	p.Summary = ""
	if p.BillingOff || p.BalanceUSD != 80 || p.AutoTopUp == nil || !p.AutoTopUp.Enabled || p.AutoTopUp.AmountUSD != 20 || p.AutoTopUp.BelowUSD != 5 ||
		!strings.HasPrefix(p.ExpectLine(), "The $1.00 card check is refunded at once.") {
		t.Errorf("prepaid %+v %+v", p, p.AutoTopUp)
	}
	p.CardNeeded, p.SkipsLeft = true, 0
	if l := p.CardLine(); !strings.HasPrefix(l, "Add a card to start more sandboxes: "+c.URL) {
		t.Errorf("card line %q", l)
	}
	p.Card = "checked"
	if l := p.CardLine(); l != "" {
		t.Errorf("checked card line %q", l)
	}
	if l := p.ExpectLine(); l != "" {
		t.Errorf("checked card expect line %q", l)
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
