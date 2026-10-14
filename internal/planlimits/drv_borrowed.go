package planlimits

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// drv_borrowed.go holds the rung-2 (experimental, opt-in) drivers. Each reads
// another app's own local state READ-ONLY (internal/planlimitslocal does the
// read; the bytes stay in memory for one refresh) and, for the token-based
// ones, sends that app's token to that vendor's private endpoint. The rules
// pinned by tests:
//
//   - a token is never refreshed, rotated or written back: an expired token
//     is ErrAuth ("open the app to sign in again"), never a refresh call;
//   - a token only ever lands in Request.SecretHeaders;
//   - no other vendor's OAuth client id is used to mint a token.
//
// Shapes come from CodexBar's decoders (MIT, discovery source only); the Go
// here is ours.

func init() {
	register(SourceClaudeOAuth, Driver{Plan: planClaudeOAuth, Parse: parseClaudeOAuth})
	register(SourceChatGPTWham, Driver{Plan: planWham, Parse: parseWham})
	register(SourceCursorApp, Driver{Plan: planCursor, Parse: parseCursor})
	register(SourceWindsurfCache, Driver{Plan: func(PlanInput) ([]Request, error) { return nil, nil }, Parse: parseWindsurf})
}

// localBytes returns a vendor-local read or ErrUnavailable.
func localBytes(m map[string][]byte, key string) ([]byte, error) {
	b, ok := m[key]
	if !ok || len(b) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, key)
	}
	return b, nil
}

// jwtClaims decodes the payload of a JWT without verifying it (only to read
// exp / sub so an expiring token is never used).
func jwtClaims(tok string) (sub string, exp int64, ok bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return "", 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", 0, false
	}
	var c struct {
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return "", 0, false
	}
	return c.Sub, c.Exp, true
}

// --- Claude: Claude Code's OAuth token against /api/oauth/usage ---------------

// claudeCreds is the projection of Claude Code's credential JSON the call
// needs. The token never leaves this struct except into SecretHeaders.
type claudeCreds struct {
	token     string
	expiresAt time.Time
	scopes    []string
	tier      string
}

func parseClaudeCreds(body []byte) (claudeCreds, error) {
	var p struct {
		O *struct {
			AccessToken      string   `json:"accessToken"`
			ExpiresAt        int64    `json:"expiresAt"` // epoch MILLISECONDS
			Scopes           []string `json:"scopes"`
			RateLimitTier    string   `json:"rateLimitTier"`
			SubscriptionType string   `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(body, &p); err != nil || p.O == nil || p.O.AccessToken == "" {
		return claudeCreds{}, fmt.Errorf("%w: claude credentials not usable", ErrAuth)
	}
	tier := p.O.RateLimitTier
	if tier == "" {
		tier = p.O.SubscriptionType
	}
	return claudeCreds{token: p.O.AccessToken, expiresAt: time.UnixMilli(p.O.ExpiresAt).UTC(), scopes: p.O.Scopes, tier: tier}, nil
}

// authHeader returns the header, or why the call must not be made. A 60 s
// margin keeps a token from expiring mid-request.
func (c claudeCreds) authHeader(now time.Time) (map[string]string, error) {
	if !now.Add(60 * time.Second).Before(c.expiresAt) {
		return nil, fmt.Errorf("%w: token expired; open Claude Code to sign in again", ErrAuth)
	}
	for _, s := range c.scopes {
		if s == "user:profile" {
			return bearer(c.token), nil
		}
	}
	return nil, fmt.Errorf("%w: token lacks the profile scope", ErrAuth)
}

// claudePlanTable maps tier substrings to plan codes, first match wins.
var claudePlanTable = []struct{ sub, code string }{
	{"max_20x", "max_20x"}, {"max_5x", "max_5x"}, {"max", "max"}, {"enterprise", "enterprise"}, {"team", "team"}, {"pro", "pro"},
}

// ClaudePlanCode derives a plan code from a Claude tier string.
func ClaudePlanCode(tier string) string {
	t := strings.ToLower(tier)
	for _, r := range claudePlanTable {
		if strings.Contains(t, r.sub) {
			return r.code
		}
	}
	return ""
}

func planClaudeOAuth(in PlanInput) ([]Request, error) {
	b, err := localBytes(in.Local, "creds")
	if err != nil {
		return nil, err
	}
	c, err := parseClaudeCreds(b)
	if err != nil {
		return nil, err
	}
	h, err := c.authHeader(in.Now)
	if err != nil {
		return nil, err
	}
	return []Request{{
		Key: "usage", Kind: ReqHTTP, Method: "GET", URL: "https://api.anthropic.com/api/oauth/usage",
		Header:        map[string]string{"Accept": "application/json", "anthropic-beta": "oauth-2025-04-20"},
		SecretHeaders: h,
	}}, nil
}

type claudeWin struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

// ParseClaudeUsage maps the /api/oauth/usage body. Null windows are absent,
// never 0%. extra_usage money is in cents.
func ParseClaudeUsage(body []byte, plan string, now time.Time) (Snapshot, error) {
	var u struct {
		FiveHour       *claudeWin `json:"five_hour"`
		SevenDay       *claudeWin `json:"seven_day"`
		SevenDayOpus   *claudeWin `json:"seven_day_opus"`
		SevenDaySonnet *claudeWin `json:"seven_day_sonnet"`
		ExtraUsage     *struct {
			IsEnabled    bool     `json:"is_enabled"`
			MonthlyLimit *float64 `json:"monthly_limit"`
			UsedCredits  *float64 `json:"used_credits"`
			Currency     string   `json:"currency"`
		} `json:"extra_usage"`
		Limits []struct {
			Kind     string   `json:"kind"`
			Percent  *float64 `json:"percent"`
			ResetsAt string   `json:"resets_at"`
			Scope    *struct {
				Model *struct {
					DisplayName string `json:"display_name"`
				} `json:"model"`
			} `json:"scope"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return Snapshot{}, shapeErr("claude usage", err)
	}
	s := newSnap(ProviderClaude, SourceClaudeOAuth, KindBorrowedToken, ConfPercentOnly, now)
	s.PlanCode = plan
	add := func(key string, w *claudeWin, length time.Duration) {
		if w == nil || w.Utilization == nil {
			return
		}
		var reset *time.Time
		if w.ResetsAt != nil {
			reset = parseISO(*w.ResetsAt)
		}
		s.Windows = append(s.Windows, Window{
			Key: key, Unit: UnitPercent, UsedFrac: fracFromPercent(*w.Utilization),
			ResetsAt: reset, Length: dur(length), Rolling: true,
		})
	}
	add("5h", u.FiveHour, 5*time.Hour)
	add("weekly", u.SevenDay, 7*24*time.Hour)
	add("model:opus", u.SevenDayOpus, 7*24*time.Hour)
	add("model:sonnet", u.SevenDaySonnet, 7*24*time.Hour)
	for _, l := range u.Limits {
		if l.Kind != "weekly_scoped" || l.Percent == nil || l.Scope == nil || l.Scope.Model == nil || l.Scope.Model.DisplayName == "" {
			continue
		}
		key := "model:" + slug(l.Scope.Model.DisplayName)
		if _, dup := FindWindow(s, key); dup {
			continue
		}
		s.Windows = append(s.Windows, Window{
			Key: key, Unit: UnitPercent, UsedFrac: fracFromPercent(*l.Percent),
			ResetsAt: parseISO(l.ResetsAt), Length: dur(7 * 24 * time.Hour), Rolling: true,
		})
	}
	if e := u.ExtraUsage; e != nil && e.IsEnabled && e.MonthlyLimit != nil && e.UsedCredits != nil && *e.MonthlyLimit > 0 {
		cur := e.Currency
		if cur == "" {
			cur = "USD"
		}
		if unit := CurrencyUnit(cur); unit != "" {
			used := float64(microsFromFloat(*e.UsedCredits / 100))
			lim := float64(microsFromFloat(*e.MonthlyLimit / 100))
			s.Windows = append(s.Windows, Window{Key: "extra_usage", Unit: unit, Used: &used, Limit: &lim, UsedFrac: F(min(used/lim, 2))})
		}
	}
	if len(s.Windows) == 0 {
		return Snapshot{}, shapeErr("claude usage: no windows", nil)
	}
	return s, nil
}

func parseClaudeOAuth(in ParseInput) (Snapshot, error) {
	body, err := need(in, "usage")
	if err != nil {
		return Snapshot{}, err
	}
	plan := ""
	if b, err := localBytes(in.Local, "creds"); err == nil {
		if c, err := parseClaudeCreds(b); err == nil {
			plan = ClaudePlanCode(c.tier)
		}
	}
	return ParseClaudeUsage(body, plan, in.Now)
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slug lower-cases a vendor label into a window-key fragment.
func slug(s string) string {
	return strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "_"), "_")
}

// --- ChatGPT: the Codex CLI's token against the web usage endpoint -------------

type codexAuth struct {
	token     string
	accountID string
	exp       time.Time
}

func parseCodexAuth(body []byte) (codexAuth, error) {
	var p struct {
		APIKey *string `json:"OPENAI_API_KEY"`
		Tokens *struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return codexAuth{}, fmt.Errorf("%w: codex auth not usable", ErrAuth)
	}
	if (p.APIKey != nil && *p.APIKey != "") || p.Tokens == nil || p.Tokens.AccessToken == "" {
		return codexAuth{}, fmt.Errorf("%w: Codex is signed in with an API key", ErrNotApplicable)
	}
	a := codexAuth{token: p.Tokens.AccessToken, accountID: p.Tokens.AccountID}
	if _, exp, ok := jwtClaims(a.token); ok && exp > 0 {
		a.exp = time.Unix(exp, 0).UTC()
	}
	return a, nil
}

var accountIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func planWham(in PlanInput) ([]Request, error) {
	b, err := localBytes(in.Local, "auth")
	if err != nil {
		return nil, err
	}
	a, err := parseCodexAuth(b)
	if err != nil {
		return nil, err
	}
	if a.exp.IsZero() || !in.Now.Add(5*time.Minute).Before(a.exp) {
		return nil, fmt.Errorf("%w: token expired; open Codex to sign in again", ErrAuth)
	}
	sec := bearer(a.token)
	if a.accountID != "" && accountIDRe.MatchString(a.accountID) {
		sec["ChatGPT-Account-Id"] = a.accountID
	}
	return []Request{{
		Key: "usage", Kind: ReqHTTP, Method: "GET", URL: "https://chatgpt.com/backend-api/wham/usage",
		Header: map[string]string{"Accept": "application/json"}, SecretHeaders: sec,
	}}, nil
}

type whamWindow struct {
	UsedPercent        *float64 `json:"used_percent"`
	LimitWindowSeconds int64    `json:"limit_window_seconds"`
	ResetAt            int64    `json:"reset_at"`
}

type whamRateLimit struct {
	LimitReached bool        `json:"limit_reached"`
	Primary      *whamWindow `json:"primary_window"`
	Secondary    *whamWindow `json:"secondary_window"`
}

// ParseWhamUsage maps the ChatGPT web usage body. Windows are classified by
// declared length, never by position.
func ParseWhamUsage(body []byte, now time.Time) (Snapshot, error) {
	var u struct {
		PlanType  string         `json:"plan_type"`
		RateLimit *whamRateLimit `json:"rate_limit"`
		Credits   *struct {
			HasCredits bool            `json:"has_credits"`
			Unlimited  bool            `json:"unlimited"`
			Balance    json.RawMessage `json:"balance"`
		} `json:"credits"`
		Additional []struct {
			LimitName      string         `json:"limit_name"`
			MeteredFeature string         `json:"metered_feature"`
			RateLimit      *whamRateLimit `json:"rate_limit"`
		} `json:"additional_rate_limits"`
	}
	if err := json.Unmarshal(body, &u); err != nil || u.RateLimit == nil {
		return Snapshot{}, shapeErr("chatgpt usage", err)
	}
	s := newSnap(ProviderChatGPT, SourceChatGPTWham, KindBorrowedToken, ConfPercentOnly, now)
	s.PlanCode = SanitizeLabel(u.PlanType)
	add := func(prefix string, rl *whamRateLimit) {
		for _, w := range []*whamWindow{rl.Primary, rl.Secondary} {
			if w == nil || w.UsedPercent == nil {
				continue
			}
			key := prefix + windowKeyForMinutes(w.LimitWindowSeconds/60)
			if _, dup := FindWindow(s, key); dup {
				continue
			}
			nw := Window{Key: key, Unit: UnitPercent, UsedFrac: fracFromPercent(*w.UsedPercent), ResetsAt: unixPtr(w.ResetAt), Rolling: true}
			if w.LimitWindowSeconds > 0 {
				nw.Length = dur(time.Duration(w.LimitWindowSeconds) * time.Second)
			}
			s.Windows = append(s.Windows, nw)
		}
	}
	add("", u.RateLimit)
	for _, a := range u.Additional {
		if a.RateLimit == nil {
			continue
		}
		id := a.MeteredFeature
		if id == "" {
			id = a.LimitName
		}
		add("model:"+slug(id)+":", a.RateLimit)
	}
	if u.RateLimit.LimitReached {
		s.Status = StatusThrottled
	}
	if c := u.Credits; c != nil && c.HasCredits && !c.Unlimited && len(c.Balance) > 0 && string(c.Balance) != "null" {
		if f, err := strconv.ParseFloat(strings.Trim(string(c.Balance), `"`), 64); err == nil {
			s.Balances = append(s.Balances, Balance{Kind: "credits", Unit: UnitCredits, Amount: int64(f)})
		}
	}
	if len(s.Windows)+len(s.Balances) == 0 {
		return Snapshot{}, shapeErr("chatgpt usage: no windows", nil)
	}
	return s, nil
}

func parseWham(in ParseInput) (Snapshot, error) {
	body, err := need(in, "usage")
	if err != nil {
		return Snapshot{}, err
	}
	return ParseWhamUsage(body, in.Now)
}

// --- Cursor: the Cursor app's token against the usage summary -----------------

var cursorUserIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// CursorCookie builds the session cookie value from the app's own JWT, or
// says why it must not be used (malformed, or within 60 s of expiry).
func CursorCookie(jwt string, now time.Time) (string, error) {
	jwt = strings.Trim(strings.TrimSpace(jwt), `"`)
	sub, exp, ok := jwtClaims(jwt)
	if !ok || sub == "" {
		return "", fmt.Errorf("%w: Cursor token not usable", ErrAuth)
	}
	if !now.Add(60 * time.Second).Before(time.Unix(exp, 0)) {
		return "", fmt.Errorf("%w: token expired; open Cursor to sign in again", ErrAuth)
	}
	uid := sub[strings.LastIndex(sub, "|")+1:]
	if !cursorUserIDRe.MatchString(uid) {
		return "", fmt.Errorf("%w: Cursor token not usable", ErrAuth)
	}
	return "WorkosCursorSessionToken=" + uid + url.QueryEscape("::") + jwt, nil
}

func planCursor(in PlanInput) ([]Request, error) {
	b, err := localBytes(in.Local, "token")
	if err != nil {
		return nil, err
	}
	c, err := CursorCookie(string(b), in.Now)
	if err != nil {
		return nil, err
	}
	return []Request{{
		Key: "summary", Kind: ReqHTTP, Method: "GET", URL: "https://cursor.com/api/usage-summary",
		Header: map[string]string{"Accept": "application/json"}, SecretHeaders: map[string]string{"Cookie": c},
	}}, nil
}

type cursorBucket struct {
	Enabled          bool     `json:"enabled"`
	Used             *float64 `json:"used"`
	Limit            *float64 `json:"limit"`
	TotalPercentUsed *float64 `json:"totalPercentUsed"`
	AutoPercentUsed  *float64 `json:"autoPercentUsed"`
	APIPercentUsed   *float64 `json:"apiPercentUsed"`
}

// cursorCentsWindow maps a bucket in CENTS onto a usd_micros window.
func cursorCentsWindow(key string, b *cursorBucket, reset, start *time.Time) (Window, bool) {
	if b == nil || !b.Enabled || b.Used == nil || b.Limit == nil || *b.Limit <= 0 {
		return Window{}, false
	}
	used, lim := *b.Used*10_000, *b.Limit*10_000
	w := Window{Key: key, Unit: UnitUSDMicros, Used: &used, Limit: &lim, UsedFrac: F(min(used/lim, 2)), ResetsAt: reset}
	if reset != nil && start != nil {
		w.Length = dur(reset.Sub(*start))
	}
	return w, true
}

// ParseCursorSummary maps /api/usage-summary. *PercentUsed values are ALREADY
// percent (0.36 means 0.36%).
func ParseCursorSummary(body []byte, now time.Time) (Snapshot, error) {
	var u struct {
		BillingCycleStart string `json:"billingCycleStart"`
		BillingCycleEnd   string `json:"billingCycleEnd"`
		MembershipType    string `json:"membershipType"`
		IsUnlimited       bool   `json:"isUnlimited"`
		Individual        *struct {
			Plan     *cursorBucket `json:"plan"`
			OnDemand *cursorBucket `json:"onDemand"`
			Overall  *cursorBucket `json:"overall"`
		} `json:"individualUsage"`
		Team *struct {
			Pooled *cursorBucket `json:"pooled"`
		} `json:"teamUsage"`
	}
	if err := json.Unmarshal(body, &u); err != nil || u.MembershipType == "" {
		return Snapshot{}, shapeErr("cursor summary", err)
	}
	s := newSnap(ProviderCursor, SourceCursorApp, KindBorrowedToken, ConfExact, now)
	s.PlanCode = SanitizeLabel(u.MembershipType)
	start, end := parseISO(u.BillingCycleStart), parseISO(u.BillingCycleEnd)
	if ind := u.Individual; ind != nil {
		if p := ind.Plan; p != nil && p.Enabled {
			if w, ok := cursorCentsWindow("cycle", p, end, start); ok {
				if p.TotalPercentUsed != nil {
					w.UsedFrac = fracFromPercent(*p.TotalPercentUsed)
				}
				s.Windows = append(s.Windows, w)
			}
			for _, x := range []struct {
				key string
				v   *float64
			}{{"cycle:auto", p.AutoPercentUsed}, {"cycle:api", p.APIPercentUsed}} {
				if x.v != nil {
					s.Windows = append(s.Windows, Window{Key: x.key, Unit: UnitPercent, UsedFrac: fracFromPercent(*x.v), ResetsAt: end})
				}
			}
		} else if w, ok := cursorCentsWindow("cycle", ind.Overall, end, start); ok {
			s.Windows = append(s.Windows, w)
		}
		if w, ok := cursorCentsWindow("on_demand", ind.OnDemand, end, start); ok {
			s.Windows = append(s.Windows, w)
		}
	}
	if u.Team != nil {
		if w, ok := cursorCentsWindow("team_pool", u.Team.Pooled, end, start); ok {
			s.Windows = append(s.Windows, w)
		}
	}
	if len(s.Windows) == 0 {
		if u.IsUnlimited {
			s.Status = StatusUnmetered
			return s, nil
		}
		return Snapshot{}, shapeErr("cursor summary: no usage buckets", nil)
	}
	return s, nil
}

func parseCursor(in ParseInput) (Snapshot, error) {
	body, err := need(in, "summary")
	if err != nil {
		return Snapshot{}, err
	}
	return ParseCursorSummary(body, in.Now)
}

// --- Windsurf: the editor's cached plan info (no token, no network) -----------

// ParseWindsurfPlanInfo maps the windsurf.settings.cachedPlanInfo value. It
// can lag the server, so the card says "as of" its time.
func ParseWindsurfPlanInfo(value []byte, now time.Time) (Snapshot, error) {
	var c struct {
		PlanName     string `json:"planName"`
		EndTimestamp int64  `json:"endTimestamp"` // epoch ms
		Usage        *struct {
			Messages          *float64 `json:"messages"`
			UsedMessages      *float64 `json:"usedMessages"`
			RemainingMessages *float64 `json:"remainingMessages"`
		} `json:"usage"`
		QuotaUsage *struct {
			DailyRemainingPercent  *float64 `json:"dailyRemainingPercent"`
			WeeklyRemainingPercent *float64 `json:"weeklyRemainingPercent"`
			DailyResetAtUnix       int64    `json:"dailyResetAtUnix"`
			WeeklyResetAtUnix      int64    `json:"weeklyResetAtUnix"`
		} `json:"quotaUsage"`
	}
	if err := json.Unmarshal(value, &c); err != nil || c.PlanName == "" {
		return Snapshot{}, shapeErr("windsurf plan info", err)
	}
	s := newSnap(ProviderWindsurf, SourceWindsurfCache, KindLocalState, ConfPercentOnly, now)
	s.PlanLabel = SanitizeLabel(c.PlanName)
	if q := c.QuotaUsage; q != nil {
		if q.DailyRemainingPercent != nil {
			s.Windows = append(s.Windows, Window{
				Key: "daily", Unit: UnitPercent, UsedFrac: F((100 - *q.DailyRemainingPercent) / 100),
				ResetsAt: unixPtr(q.DailyResetAtUnix), Length: dur(24 * time.Hour),
			})
		}
		if q.WeeklyRemainingPercent != nil {
			s.Windows = append(s.Windows, Window{
				Key: "weekly", Unit: UnitPercent, UsedFrac: F((100 - *q.WeeklyRemainingPercent) / 100),
				ResetsAt: unixPtr(q.WeeklyResetAtUnix), Length: dur(7 * 24 * time.Hour),
			})
		}
	} else if u := c.Usage; u != nil && u.Messages != nil && *u.Messages > 0 {
		used := u.UsedMessages
		if used == nil && u.RemainingMessages != nil {
			used = F(*u.Messages - *u.RemainingMessages)
		}
		if used != nil {
			var end *time.Time
			if c.EndTimestamp > 0 {
				t := time.UnixMilli(c.EndTimestamp).UTC()
				end = &t
			}
			s.Windows = append(s.Windows, Window{
				Key: "cycle:messages", Unit: UnitRequests, Used: used, Limit: u.Messages,
				UsedFrac: F(*used / *u.Messages), ResetsAt: end,
			})
		}
	}
	if len(s.Windows) == 0 {
		return Snapshot{}, shapeErr("windsurf plan info: no quota", nil)
	}
	return s, nil
}

func parseWindsurf(in ParseInput) (Snapshot, error) {
	b, err := localBytes(in.Local, "plan")
	if err != nil {
		return Snapshot{}, err
	}
	return ParseWindsurfPlanInfo(b, in.Now)
}
