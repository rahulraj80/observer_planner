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
	// CodeBalanceEmpty: the prepaid balance is used up and auto top-up is
	// off (SandHold, 2026-11-13).
	CodeBalanceEmpty = "balance_empty"
	// CodeTopUpFailed: the card was declined on an auto top-up.
	CodeTopUpFailed = "top_up_failed"
	// CodeFreeSandboxesUsed: on a node with billing off, the account
	// started all its free sandboxes.
	CodeFreeSandboxesUsed = "free_sandboxes_used"
)

// PlanRefusal reports whether a refusal is the account's plan (a card is
// needed, the free month ended, the credit or balance is used up, a top-up
// was declined, the free sandboxes are used), which only the person (or the
// node's operator) can fix.
func PlanRefusal(err error) (*APIError, bool) {
	var ae *APIError
	if !errors.As(err, &ae) {
		return nil, false
	}
	switch ae.Code {
	case CodeCardRequired, CodeFreeMonthEnded, CodeCreditUsedUp, CodeBalanceEmpty, CodeTopUpFailed, CodeFreeSandboxesUsed:
		return ae, true
	}
	return nil, false
}

// FreeLimits are the limits a node puts on free use: each sandbox's run
// time and size, how many at once, a day and (billing off) in all.
type FreeLimits struct {
	Kind          string `json:"kind"`
	MaxRunMinutes int    `json:"maxRunMinutes"`
	CPU           string `json:"cpu"`
	Memory        string `json:"memory"`
	Disk          string `json:"disk"`
	Network       string `json:"network"`
	AtOnce        int    `json:"atOnce"`
	PerDay        int    `json:"perDay"`
	Total         int    `json:"total,omitempty"`
}

// PublicOffer is the offer as a node sets it, before an account exists.
// A node with billing off offers a number of free sandboxes in all and no
// free month; Expect is the node's own words on what to expect.
type PublicOffer struct {
	FreeDays      int         `json:"freeDays"`
	AllowanceUSD  float64     `json:"allowanceUsd"`
	CardSkips     int         `json:"cardSkips"`
	CardCheckUSD  float64     `json:"cardCheckUsd"`
	BillingOff    bool        `json:"billingOff"`
	FreeSandboxes int         `json:"freeSandboxes,omitempty"`
	FreeLimits    *FreeLimits `json:"freeLimits,omitempty"`
	TopUpUSD      float64     `json:"topUpUsd,omitempty"`
	TopUpBelowUSD float64     `json:"topUpBelowUsd,omitempty"`
	Expect        string      `json:"expect,omitempty"`
}

// Line is the offer in one sentence ("" when the node has no free month
// and no free sandboxes).
// no-bare-usd:allow (SandHold's plan terms, not a developer's coding spend)
func (o *PublicOffer) Line() string {
	if o == nil {
		return ""
	}
	if o.BillingOff {
		if o.FreeSandboxes <= 0 {
			return ""
		}
		s := fmt.Sprintf("Billing is off on this node: %d free sandboxes in all, no card taken.", o.FreeSandboxes)
		if l := o.FreeLimits; l != nil && l.MaxRunMinutes > 0 {
			s = fmt.Sprintf("Billing is off on this node: %d free sandboxes in all (each up to %d min, %s vCPU, %s memory, %d at a time), no card taken.",
				o.FreeSandboxes, l.MaxRunMinutes, l.CPU, strings.Replace(l.Memory, "Gi", " GiB", 1), l.AtOnce)
		}
		return s
	}
	if o.FreeDays <= 0 {
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
	if out.Offer != nil {
		out.Offer.Expect = PlainLine(out.Offer.Expect)
	}
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
	// Expect is the node's words on what to expect at the card check (or,
	// billing off, from the free sandboxes).
	Expect            string      `json:"expect,omitempty"`
	BillingOff        bool        `json:"billingOff"`
	FreeSandboxes     int         `json:"freeSandboxes,omitempty"`
	FreeSandboxesLeft int         `json:"freeSandboxesLeft,omitempty"`
	FreeUse           bool        `json:"freeUse,omitempty"`
	FreeLimits        *FreeLimits `json:"freeLimits,omitempty"`
	AutoTopUp         *AutoTopUp  `json:"autoTopUp,omitempty"`
	BalanceUSD        float64     `json:"balanceUsd"`
	// CheckedAt is when Observer read it (not the node's).
	CheckedAt time.Time `json:"checkedAt,omitzero"`
}

// AutoTopUp is the account's auto top-up: the card is charged AmountUSD
// whenever the balance falls below BelowUSD, while Enabled.
type AutoTopUp struct {
	Enabled   bool    `json:"enabled"`
	AmountUSD float64 `json:"amountUsd"`
	BelowUSD  float64 `json:"belowUsd"`
	MinUSD    float64 `json:"minUsd,omitempty"`
	MaxUSD    float64 `json:"maxUsd,omitempty"`
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
	p.AddCardURL, p.Expect = PlainLine(p.AddCardURL), PlainLine(p.Expect)
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
	left := fmt.Sprintf("%d of %d sandboxes left", p.SkipsLeft, p.CardSkips)
	if strings.Contains(p.Summary, left) {
		// The node's summary already named the sandboxes left; repeat only
		// what it did not (where to add a card, and the check's terms).
		return fmt.Sprintf("Add a card any time at %s (%s).", p.AddCardURL, check)
	}
	return fmt.Sprintf("%s before a card is needed; add one any time at %s (%s).", left, p.AddCardURL, check)
}

// LimitsLine names the limits the node puts on free use, from freeLimits
// ("" when the node did not send them). It is the same line the dashboard's
// SandHold plan box shows.
func (p *Plan) LimitsLine() string {
	if p == nil || p.FreeLimits == nil || p.FreeLimits.MaxRunMinutes <= 0 {
		return ""
	}
	l := p.FreeLimits
	parts := []string{fmt.Sprintf("each sandbox up to %d min", l.MaxRunMinutes)}
	if l.CPU != "" {
		parts = append(parts, l.CPU+" vCPU")
	}
	if l.Memory != "" {
		parts = append(parts, strings.Replace(l.Memory, "Gi", " GiB", 1)+" memory")
	}
	if l.Disk != "" {
		parts = append(parts, strings.Replace(l.Disk, "Gi", " GiB", 1)+" disk")
	}
	if l.AtOnce > 0 {
		parts = append(parts, fmt.Sprintf("%d at a time", l.AtOnce))
	}
	if l.PerDay > 0 {
		parts = append(parts, fmt.Sprintf("%d a day", l.PerDay))
	}
	if l.Total > 0 {
		parts = append(parts, fmt.Sprintf("%d in all", l.Total))
	}
	return "Free use: " + strings.Join(parts, ", ") + "."
}

// ExpectLine is what to expect, in the node's words ("" when it says
// nothing, or a checked card on a node with billing on: then it was said
// already).
func (p *Plan) ExpectLine() string {
	if p == nil || (!p.BillingOff && p.Card == "checked") {
		return ""
	}
	return p.Expect
}
