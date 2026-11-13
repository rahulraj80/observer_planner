package sandhold

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// offer.go is SandHold's first-month offer for one person (SandHold note 44,
// 2026-11-13): a self-serve account's first month is free up to the starter
// credit, it may start a few sandboxes before it adds a card, and a card is
// checked with a small charge that is refunded at once. Observer shows the
// offer before connecting (GET /app/api/signup, no key) and where the
// account stands after (GET /v1/offer, any key of the account).

// DefaultNode is the public SandHold node `observer sandbox connect` uses
// when no node is named. EnvDefaultURL overrides it.
const DefaultNode = "https://sandhold-sb.dynv6.net:48000"

// EnvDefaultURL names another default node (a node of your own, a test
// node).
const EnvDefaultURL = "SANDHOLD_DEFAULT_URL"

// The refusal codes of the offer's create gate.
const (
	// CodeCardRequired: the account started the sandboxes it may start
	// without a card; the fix names the page that adds one.
	CodeCardRequired = "card_required"
	// CodeFreeMonthEnded: the free month is over and no card is on file.
	CodeFreeMonthEnded = "free_month_ended"
	// CodeCreditUsedUp: the credit (the free month's fair use) is used up
	// and no card is on file.
	CodeCreditUsedUp = "credit_used_up"
)

// PlanRefusal reports whether a refusal is the account's plan (a card is
// needed, the free month ended, the credit is used up), which only the
// person can fix, in the node's portal.
func PlanRefusal(err error) (*APIError, bool) {
	var ae *APIError
	if !errors.As(err, &ae) {
		return nil, false
	}
	switch ae.Code {
	case CodeCardRequired, CodeFreeMonthEnded, CodeCreditUsedUp:
		return ae, true
	}
	return nil, false
}

// PublicOffer is the offer as a node sets it, before an account exists.
type PublicOffer struct {
	FreeDays     int     `json:"freeDays"`
	AllowanceUSD float64 `json:"allowanceUsd"`
	CardSkips    int     `json:"cardSkips"`
	CardCheckUSD float64 `json:"cardCheckUsd"`
}

// Line is the offer in one sentence ("" when the node has no free month).
// no-bare-usd:allow (SandHold's plan terms, not a developer's coding spend)
func (o *PublicOffer) Line() string {
	if o == nil || o.FreeDays <= 0 {
		return ""
	}
	s := fmt.Sprintf("First month free: %d days at no charge, up to $%.0f of use.", o.FreeDays, o.AllowanceUSD)
	if o.CardSkips > 0 {
		s += fmt.Sprintf(" No card needed for your first %d sandboxes; after that a card is checked with a $%.2f charge, refunded at once.", o.CardSkips, o.CardCheckUSD)
	} else {
		s += fmt.Sprintf(" A card is checked with a $%.2f charge, refunded at once.", o.CardCheckUSD)
	}
	return s
}

// SignupInfo is GET /app/api/signup: whether the node takes sign-ups and
// its offer.
type SignupInfo struct {
	SelfServe      bool         `json:"selfServe"`
	SignupClosed   bool         `json:"signupClosed"`
	InviteAccepted bool         `json:"inviteAccepted"`
	ClosedReason   string       `json:"closedReason"`
	Offer          *PublicOffer `json:"offer"`
}

// Signup reads the node's sign-up page facts (no key). A node without
// self-serve sign-up answers 404: nil and no error.
func (c *Client) Signup(ctx context.Context) (*SignupInfo, error) {
	var out SignupInfo
	err := c.Do(ctx, http.MethodGet, "/app/api/signup", nil, &out)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out.ClosedReason = PlainLine(out.ClosedReason)
	return &out, nil
}

// Plan is where a self-serve account stands in the offer (GET /v1/offer).
type Plan struct {
	FreeDays         int        `json:"freeDays"`
	FreeUntil        *time.Time `json:"freeUntil,omitempty"`
	InFreeMonth      bool       `json:"inFreeMonth"`
	DaysLeft         int        `json:"daysLeft"`
	AllowanceUSD     float64    `json:"allowanceUsd"`
	AllowanceUsedUSD float64    `json:"allowanceUsedUsd"`
	CardGate         bool       `json:"cardGate"`
	CardSkips        int        `json:"cardSkips"`
	SandboxesCreated int        `json:"sandboxesCreated"`
	SkipsLeft        int        `json:"skipsLeft"`
	CardNeeded       bool       `json:"cardNeeded"`
	// Card is none, checked, unchecked or failed.
	Card           string  `json:"card"`
	CardCheckUSD   float64 `json:"cardCheckUsd"`
	CardCheckError string  `json:"cardCheckError,omitempty"`
	AddCardURL     string  `json:"addCardUrl"`
	Summary        string  `json:"summary"`
	// CheckedAt is when Observer read it (not the node's).
	CheckedAt time.Time `json:"checkedAt,omitzero"`
}

// Offer reads the account's plan; nil and no error for an org without
// one (404 no_offer, or a node older than the offer).
func (c *Client) Offer(ctx context.Context) (*Plan, error) {
	var p Plan
	err := c.Do(ctx, http.MethodGet, "/v1/offer", nil, &p)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Card, p.CardCheckError, p.Summary = PlainLine(p.Card), PlainLine(p.CardCheckError), PlainLine(p.Summary)
	p.AddCardURL = PlainLine(p.AddCardURL)
	if !strings.HasPrefix(p.AddCardURL, c.URL+"/") {
		// Only the node's own page is ever shown as where to add a card.
		p.AddCardURL = c.URL + "/app/#/billing"
	}
	return &p, nil
}

// CardLine says what the person may do about a card, or "" when nothing
// is needed (a checked card, or a node without the card gate).
func (p *Plan) CardLine() string {
	if p == nil || !p.CardGate || p.Card == "checked" {
		return ""
	}
	check := fmt.Sprintf("checked with a $%.2f charge, refunded at once", p.CardCheckUSD)
	switch {
	case p.CardNeeded && p.Card == "failed":
		return fmt.Sprintf("The last card's check failed (%s). Add another card to start more sandboxes: %s (%s).", p.CardCheckError, p.AddCardURL, check)
	case p.CardNeeded:
		return fmt.Sprintf("Add a card to start more sandboxes: %s (%s).", p.AddCardURL, check)
	}
	return fmt.Sprintf("%d of %d sandboxes left before a card is needed; add one any time at %s (%s).", p.SkipsLeft, p.CardSkips, p.AddCardURL, check)
}
