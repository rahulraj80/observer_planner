package demo

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// Seed writes a deterministic N-day / N-user synthetic activity set
// straight into the local database through the store seam: projects,
// sessions, api_turns (proxy tools), token_usage + actions (watcher tools),
// and advise-mode router_decisions (so the briefing's routing-shadow
// opportunity has evidence). Past days end at YESTERDAY (so the briefing's
// default window is full); FutureDays are dated from today forward so a demo
// keeps working for the next N days without re-seeding. Re-running is safe:
// every row this generator writes carries an identifiable marker and is
// purged first.
//
// All seeded cost_usd / estimated_cost_usd values are left at zero on
// purpose — every dashboard surface prices per-turn through the cost engine
// (recorded-0 means "reprice me"), which also makes the briefing loader
// compute long-context surcharge and cache-savings attribution.
func Seed(ctx context.Context, database *sql.DB, opts SeedOptions) (SeedResult, error) {
	if opts.Days <= 0 {
		opts.Days = 7
	}
	if opts.FutureDays < 0 {
		opts.FutureDays = 0
	}
	if opts.Users <= 0 {
		opts.Users = 30
	}
	if opts.Users > len(seedUsers) {
		opts.Users = len(seedUsers)
	}
	g := &gen{
		opts: opts,
		//nolint:gosec // G404: deterministic seed (seedRNGSeed) by design — the seeder must
		// produce a byte-identical dataset across runs; crypto/rand would break re-run idempotency.
		rng:     rand.New(rand.NewSource(seedRNGSeed)),
		engine:  cost.NewEngine(config.IntelligenceConfig{}),
		st:      store.New(database),
		db:      database,
		projIDs: map[string]int64{},
	}
	g.users = seedUsers[:opts.Users]
	g.projects = seedProjects

	if err := g.purge(ctx); err != nil {
		return SeedResult{}, err
	}

	for i := range g.projects {
		id, err := g.st.UpsertProject(ctx, g.projects[i].root, g.projects[i].remote)
		if err != nil {
			return SeedResult{}, fmt.Errorf("upsert project %s: %w", g.projects[i].root, err)
		}
		g.projIDs[g.projects[i].root] = id
	}
	g.res.Projects = len(g.projects)

	todayMid := localMidnight(time.Now())
	for d := 0; d < opts.Days; d++ {
		// Completed days only: the newest generated day is YESTERDAY, so the
		// briefing's default window (most recent completed local day) is full.
		dayStart := todayMid.Add(-time.Duration(opts.Days-d) * 24 * time.Hour)
		isYesterday := d == opts.Days-1
		if err := g.seedDay(ctx, dayStart, isYesterday); err != nil {
			return g.res, err
		}
	}
	// Future-dated demo days: from TODAY forward, so navigating the briefing
	// / overview date pickers keeps showing full data for the next
	// FutureDays days. Router advice is seeded on future days too so the
	// session drill-downs carry the same routing detail as yesterday's.
	for d := 1; d <= opts.FutureDays; d++ {
		dayStart := todayMid.Add(time.Duration(d) * 24 * time.Hour)
		if err := g.seedDay(ctx, dayStart, true); err != nil {
			return g.res, err
		}
	}
	// Rolling 24h advise-shadow evidence, independent of the day loop, so
	// the routing-shadow opportunity fires no matter what time the seed runs.
	if err := g.seedShadowWindow(ctx); err != nil {
		return g.res, err
	}
	g.res.Days = opts.Days
	g.res.FutureDays = opts.FutureDays
	g.res.Users = len(g.users)
	g.res.EstimatedUSD = math.Round(g.res.EstimatedUSD*100) / 100
	return g.res, nil
}

// SeedOptions controls the synthetic activity volume.
type SeedOptions struct {
	Days       int // number of completed local days to generate (default 7)
	FutureDays int // future-dated demo days to generate from today forward (default 0)
	Users      int // number of synthetic users (default 30)
}

// SeedResult reports what was written.
type SeedResult struct {
	Days         int     `json:"days"`
	FutureDays   int     `json:"future_days"`
	Users        int     `json:"users"`
	Projects     int     `json:"projects"`
	Sessions     int     `json:"sessions"`
	APITurns     int     `json:"api_turns"`
	TokenEvents  int     `json:"token_events"`
	Actions      int     `json:"actions"`
	RouterRows   int     `json:"router_decisions"`
	EstimatedUSD float64 `json:"estimated_cost_usd"`
}

const (
	seedRNGSeed      = 0xC0FFEE
	seedIDPrefix     = "seed-"
	seedSourceFilePF = "demo-seed://"
	seedSource       = "demo-seed"
	seedPolicy       = "demo-seed"
	seedBranch       = "main"
	seedRoutePolicy  = "demo-seed-v1"
)

type gen struct {
	opts     SeedOptions
	rng      *rand.Rand
	engine   *cost.Engine
	st       *store.Store
	db       *sql.DB
	users    []userInfo
	projects []projectInfo
	projIDs  map[string]int64
	res      SeedResult
}

type userInfo struct {
	name  string
	email string
	proj  int
}

type projectInfo struct {
	root   string
	remote string
}

var seedProjects = []projectInfo{
	{root: "/work/atlas-api", remote: "github.com/acme/atlas-api"},
	{root: "/work/webapp", remote: "github.com/acme/webapp"},
	{root: "/work/infra", remote: "github.com/acme/infra"},
	{root: "/work/mobile-app", remote: "github.com/acme/mobile-app"},
	{root: "/work/data-pipeline", remote: "github.com/acme/data-pipeline"},
	{root: "/work/docs-site", remote: "github.com/acme/docs-site"},
}

var seedUsers = func() []userInfo {
	names := []string{
		"Alice", "Ben", "Carla", "Daniel", "Emma", "Farid", "Grace", "Hiro", "Ines", "Javier",
		"Kate", "Liam", "Mia", "Noah", "Olivia", "Pavel", "Quinn", "Rita", "Sam", "Tara",
		"Umberto", "Vicky", "Walter", "Ximena", "Yusuf", "Zoe", "Aaron", "Beatrice", "Chen", "Diana",
	}
	out := make([]userInfo, len(names))
	for i, n := range names {
		out[i] = userInfo{
			name:  n,
			email: strings.ToLower(n) + "@example.com",
			proj:  i % len(seedProjects),
		}
	}
	return out
}()

// weightedModel carries a model weight within a harness.
type weightedModel struct {
	id     string
	weight int
}

// harnessSpec is one AI surface. channel "proxy" produces api_turns (the
// observer reverse proxy path); "watcher" produces token_usage + actions.
type harnessSpec struct {
	tool       string
	kind       string // coding | chat | batch
	channel    string // proxy | watcher
	weight     int
	models     []weightedModel
	miniModels []string // premium-only set for trivial-model mini sessions
}

func wm(id string, w int) weightedModel { return weightedModel{id: id, weight: w} }

var (
	codingModels = []weightedModel{
		wm("claude-sonnet-4-6", 30), wm("claude-opus-4-8", 8), wm("claude-sonnet-4-20250514", 10),
		wm("claude-opus-5", 6), wm("claude-haiku-4-5", 14), wm("claude-opus-4", 8),
		wm("gpt-5.2-codex", 20), wm("gpt-5.1-codex", 12), wm("gpt-5-codex", 8), wm("gpt-5.2", 8),
		wm("gpt-5.5", 6), wm("gpt-5.4-mini", 8), wm("gpt-5-mini", 5),
		wm("gemini-2.5-pro", 12), wm("gemini-3.1-pro-preview", 8), wm("gemini-2.5-flash", 10),
		wm("gemini-3-flash", 6), wm("grok-4.5", 6), wm("grok-build-0.1", 4), wm("grok-code", 4),
		wm("kimi-k3", 6), wm("kimi-k2-6", 5), wm("qwen3-coder", 5), wm("qwen3-max", 4),
		wm("qwen3.8-max-preview", 3), wm("deepseek-v4-pro", 8), wm("deepseek-v4-flash", 6),
		wm("glm-5.2", 5), wm("mistral-large", 4), wm("minimax-m3", 4), wm("gpt-oss-120b", 4),
	}

	// Workday agentic harnesses (75% of active time).
	seedCodingHarnesses = []harnessSpec{
		{tool: models.ToolClaudeCode, kind: "coding", channel: "proxy", weight: 30, models: codingModels},
		{tool: models.ToolCodex, kind: "coding", channel: "proxy", weight: 25, models: codingModels},
		{tool: models.ToolCursor, kind: "coding", channel: "watcher", weight: 20, models: codingModels},
		{tool: models.ToolGeminiCLI, kind: "coding", channel: "watcher", weight: 15, models: codingModels},
		{tool: models.ToolOpenCode, kind: "coding", channel: "watcher", weight: 10, models: codingModels},
	}

	// Workday chat/assistant harnesses (25% of active time).
	seedChatHarnesses = []harnessSpec{
		{
			tool: models.ToolChatGPTWeb, kind: "chat", channel: "watcher", weight: 18,
			models:     []weightedModel{wm("gpt-5.2", 35), wm("gpt-5.4", 12), wm("gpt-5-mini", 33), wm("o4-mini", 20)},
			miniModels: []string{"o4-mini"},
		},
		{
			tool: models.ToolClaudeWeb, kind: "chat", channel: "watcher", weight: 16,
			models:     []weightedModel{wm("claude-sonnet-4-6", 40), wm("claude-opus-5", 15), wm("claude-haiku-4-5", 30), wm("claude-sonnet-5", 15)},
			miniModels: []string{"claude-sonnet-4-6", "claude-opus-5"},
		},
		{
			tool: models.ToolCopilot, kind: "chat", channel: "watcher", weight: 20,
			models: []weightedModel{wm("claude-haiku-4-5", 30), wm("claude-sonnet-4-6", 20), wm("gpt-5-mini", 30), wm("gpt-4.1-nano", 20)},
		},
		{
			tool: models.ToolGeminiWeb, kind: "chat", channel: "watcher", weight: 12,
			models:     []weightedModel{wm("gemini-3.1-pro-preview", 28), wm("gemini-2.5-flash", 47), wm("gemini-3-flash", 25)},
			miniModels: []string{"gemini-2.5-pro"},
		},
		{
			tool: models.ToolPerplexityWeb, kind: "chat", channel: "watcher", weight: 14,
			models: []weightedModel{wm("claude-sonnet-4-6", 30), wm("gpt-5.4-mini", 30), wm("gpt-5-mini", 25), wm("gemini-2.5-flash", 15)},
		},
		{
			tool: models.ToolGrok, kind: "chat", channel: "watcher", weight: 12,
			models: []weightedModel{wm("grok-4.5", 80), wm("grok-code", 20)},
		},
	}

	// Off-hours batch / eval / research surfaces.
	seedBatchHarnesses = []harnessSpec{
		{
			tool: "eval-batch", kind: "batch", channel: "watcher", weight: 1,
			models: []weightedModel{wm("deepseek-v4-flash", 40), wm("glm-5.1", 20), wm("gpt-oss-120b", 20), wm("kimi-k2-6", 20)},
		},
		{
			tool: "batch-jobs", kind: "batch", channel: "watcher", weight: 1,
			models: []weightedModel{wm("deepseek-v4-pro", 35), wm("kimi-k2-6", 20), wm("glm-5.2", 20), wm("minimax-m3", 10), wm("step-3.5-flash", 15)},
		},
		{
			tool: "research-agent", kind: "batch", channel: "watcher", weight: 1,
			models: []weightedModel{wm("gemini-2.5-pro", 30), wm("gpt-5.2", 20), wm("claude-sonnet-4-6", 20), wm("deepseek-v4-pro", 15), wm("grok-4.5", 15)},
		},
	}
)

// purge removes every previously seeded row in FK-safe order. All rows this
// generator creates are identifiable by a seed marker.
func (g *gen) purge(ctx context.Context) error {
	stmts := []string{
		`DELETE FROM router_decisions WHERE policy_name = '` + seedPolicy + `'`,
		`DELETE FROM actions WHERE source_file LIKE '` + seedSourceFilePF + `%'`,
		`DELETE FROM api_turns WHERE source = '` + seedSource + `'`,
		`DELETE FROM token_usage WHERE source_file LIKE '` + seedSourceFilePF + `%'`,
		`DELETE FROM sessions WHERE id LIKE '` + seedIDPrefix + `%'`,
		`DELETE FROM projects WHERE root_path LIKE '/work/%'`,
	}
	for _, s := range stmts {
		if _, err := g.db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("seed purge: %w", err)
		}
	}
	return nil
}

func localMidnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// seedDay generates one local day: per-active-user workday + off-hours batch.
// withRouter enables per-session advise-mode router_decisions (yesterday and
// future demo days).
func (g *gen) seedDay(ctx context.Context, dayStart time.Time, withRouter bool) error {
	weekend := dayStart.Weekday() == time.Saturday || dayStart.Weekday() == time.Sunday
	for i := range g.users {
		if weekend && i%4 != 0 {
			continue // ~25% of the team works a half day on weekends
		}
		u := g.users[i]
		if err := g.userDay(ctx, u, dayStart, weekend, withRouter); err != nil {
			return err
		}
	}
	if !weekend {
		for i := 0; i < len(g.users); i += 5 {
			if err := g.batchNight(ctx, g.users[i], dayStart); err != nil {
				return err
			}
		}
	}
	return nil
}

// userDay schedules a day of interleaved agentic + chat sessions.
func (g *gen) userDay(ctx context.Context, u userInfo, dayStart time.Time, weekend, withRouter bool) error {
	var start, end time.Time
	if weekend {
		start = dayStart.Add(10*time.Hour + time.Duration(g.rng.Intn(40))*time.Minute)
		end = dayStart.Add(13*time.Hour + 45*time.Minute)
	} else {
		start = dayStart.Add(8*time.Hour + 45*time.Minute + time.Duration(g.rng.Intn(30))*time.Minute)
		end = dayStart.Add(17*time.Hour + 30*time.Minute + time.Duration(g.rng.Intn(10))*time.Minute)
	}
	if end.Sub(start) < 45*time.Minute {
		end = start.Add(45 * time.Minute)
	}
	for _, s := range g.schedule(u, dayStart, start, end, weekend) {
		if err := g.writeSession(ctx, s, withRouter); err != nil {
			return err
		}
	}
	return nil
}

// schedule builds the session list for one user's workday. Chat sessions are
// short and frequent so they consume ~25% of active time.
func (g *gen) schedule(u userInfo, dayStart, start, end time.Time, weekend bool) []*sessionInfo {
	pChat := 0.68
	if weekend {
		pChat = 0.75
	}
	lunchStart := dayStart.Add(12*time.Hour + 30*time.Minute)
	lunchEnd := dayStart.Add(13*time.Hour + 30*time.Minute)

	var out []*sessionInfo
	t := start
	seq := 0
	for t.Before(end) {
		if t.After(lunchStart) && t.Before(lunchEnd) {
			t = lunchEnd
			if t.After(end) {
				break
			}
		}
		remaining := end.Sub(t)
		if remaining < 3*time.Minute {
			break
		}
		isChat := g.rng.Float64() < pChat
		var hs harnessSpec
		if isChat {
			hs = g.pick(seedChatHarnesses)
		} else {
			hs = g.pick(seedCodingHarnesses)
		}
		var dur time.Duration
		if isChat {
			dur = time.Duration(9+g.rng.Intn(10)) * time.Minute
		} else {
			dur = time.Duration(35+g.rng.Intn(46)) * time.Minute
		}
		if dur > remaining {
			dur = remaining
		}
		mini := false
		if isChat && len(hs.miniModels) > 0 && g.rng.Intn(100) < 12 {
			mini = true
		}
		s := &sessionInfo{
			id:       seedIDPrefix + u.email[:strings.IndexByte(u.email, '@')] + "-" + dayStart.Format("20060102") + "-" + strconv.Itoa(seq),
			tool:     hs.tool,
			kind:     hs.kind,
			channel:  hs.channel,
			harness:  hs,
			project:  g.projects[u.proj].root,
			user:     u,
			start:    t,
			end:      t.Add(dur),
			mini:     mini,
			lowCache: g.lowCacheDay(dayStart),
		}
		s.turns = g.makeTurns(s)
		out = append(out, s)
		seq++
		t = t.Add(dur + time.Duration(4+g.rng.Intn(13))*time.Minute)
	}
	return out
}

// batchNight adds off-hours eval / batch / research sessions for a user.
func (g *gen) batchNight(ctx context.Context, u userInfo, dayStart time.Time) error {
	hs := g.pick(seedBatchHarnesses)
	for i := 0; i < 1+g.rng.Intn(2); i++ {
		start := dayStart.Add(22*time.Hour + time.Duration(g.rng.Intn(90))*time.Minute)
		dur := time.Duration(25+g.rng.Intn(50)) * time.Minute
		s := &sessionInfo{
			id:       seedIDPrefix + u.email[:strings.IndexByte(u.email, '@')] + "-" + dayStart.Format("20060102") + "-off" + strconv.Itoa(i),
			tool:     hs.tool,
			kind:     hs.kind,
			channel:  hs.channel,
			harness:  hs,
			project:  g.projects[u.proj].root,
			user:     u,
			start:    start,
			end:      start.Add(dur),
			lowCache: g.lowCacheDay(dayStart),
		}
		s.turns = g.makeTurns(s)
		if err := g.writeSession(ctx, s, false); err != nil {
			return err
		}
	}
	return nil
}

// lowCacheDay marks one mid-window day as cache-hostile so the cache-hit-rate
// opportunity card has evidence.
func (g *gen) lowCacheDay(dayStart time.Time) bool {
	return dayStart.Weekday() == time.Wednesday || dayStart.Weekday() == time.Sunday
}

func (g *gen) pick(hs []harnessSpec) harnessSpec {
	total := 0
	for _, h := range hs {
		total += h.weight
	}
	n := g.rng.Intn(total)
	for _, h := range hs {
		n -= h.weight
		if n < 0 {
			return h
		}
	}
	return hs[len(hs)-1]
}

func (g *gen) pickModel(hs harnessSpec, mini bool) string {
	if mini && len(hs.miniModels) > 0 {
		return hs.miniModels[g.rng.Intn(len(hs.miniModels))]
	}
	total := 0
	for _, m := range hs.models {
		total += m.weight
	}
	n := g.rng.Intn(total)
	for _, m := range hs.models {
		n -= m.weight
		if n < 0 {
			return m.id
		}
	}
	return hs.models[len(hs.models)-1].id
}

type sessionInfo struct {
	id       string
	tool     string
	kind     string
	channel  string
	harness  harnessSpec
	project  string
	user     userInfo
	start    time.Time
	end      time.Time
	mini     bool
	lowCache bool
	turns    []turnInfo
}

type turnInfo struct {
	ts        time.Time
	model     string
	input     int64
	output    int64
	cacheRead int64
	cacheCrea int64
	cache1h   int64
	reasoning int64
	webSearch int64
	fast      bool
	msgCount  int
	toolUse   int
	ttft      int64
	totalMS   int64
	stop      string
	http      int
	errClass  string
	errMsg    string
	action    *models.ToolEvent
}

// makeTurns generates the per-session turn timeline. Inter-arrival is ≥20s
// (max 3 requests/minute), with session-length average throughput below that.
func (g *gen) makeTurns(s *sessionInfo) []turnInfo {
	var base float64
	switch {
	case s.kind == "coding":
		base = 1.0
	case s.harness.tool == models.ToolCopilot:
		base = 2.0
	case s.kind == "batch":
		base = 1.6
	default: // chat
		base = 0.55
	}
	span := s.end.Sub(s.start)
	n := int(span.Minutes() * base * (0.75 + g.rng.Float64()*0.5))
	if n < 1 {
		n = 1
	}
	if s.mini {
		n = 1 + g.rng.Intn(4)
	}
	interval := span / time.Duration(n)
	out := make([]turnInfo, 0, n)
	prev := s.start.Add(-time.Second)
	for i := 0; i < n; i++ {
		ts := s.start.Add(time.Duration(i) * interval)
		jit := time.Duration((g.rng.Float64()*0.6 - 0.3) * float64(interval))
		ts = ts.Add(jit)
		if ts.Sub(prev) < 20*time.Second {
			ts = prev.Add(20 * time.Second)
		}
		if ts.After(s.end) {
			ts = s.end
		}
		t := g.makeTurn(s, ts, i)
		out = append(out, t)
		prev = ts
	}
	return out
}

func (g *gen) between(min, max float64) float64 {
	if max <= min {
		return min
	}
	return min + g.rng.Float64()*(max-min)
}

func (g *gen) makeTurn(s *sessionInfo, ts time.Time, i int) turnInfo {
	model := g.pickModel(s.harness, s.mini)
	p, ok := g.engine.Lookup(model)
	if !ok {
		p = cost.Pricing{}
	}
	t := turnInfo{ts: ts, model: model, msgCount: 1 + g.rng.Intn(2), stop: "end_turn"}

	// Proxy-channel failures: recorded as error api_turns (no token rows).
	if s.channel == "proxy" && !s.mini && g.rng.Intn(100) < 3 {
		t.http = 500
		t.errClass = "overloaded_error"
		t.errMsg = "The upstream model is temporarily overloaded."
		t.stop = "error"
		t.totalMS = 900 + g.rng.Int63n(4000)
		return t
	}

	var totalPrompt int64
	var freshMin, freshMax float64
	var outMin, outMax int64
	var creaMin, creaMax int64
	var webPct int
	switch s.kind {
	case "coding":
		totalPrompt = int64(g.between(5000, 28000))
		if s.lowCache {
			freshMin, freshMax = 0.55, 0.75
		} else {
			freshMin, freshMax = 0.15, 0.27
		}
		outMin, outMax = 300, 2400
		creaMin, creaMax = 400, 5000
		webPct = 2
		t.toolUse = 1 + g.rng.Intn(3)
		t.stop = "tool_use"
	case "chat":
		if s.mini {
			totalPrompt = int64(g.between(2000, 9000))
			freshMin, freshMax = 0.55, 0.8
			outMin, outMax = 200, 2200
			creaMin, creaMax = 0, 800
			webPct = 3
		} else {
			totalPrompt = int64(g.between(1200, 7500))
			freshMin, freshMax = 0.35, 0.55
			outMin, outMax = 150, 1500
			creaMin, creaMax = 0, 1500
			webPct = 6
		}
	case "batch":
		totalPrompt = int64(g.between(2000, 26000))
		freshMin, freshMax = 0.6, 0.85
		outMin, outMax = 100, 900
		creaMin, creaMax = 300, 3000
		webPct = 8
	case "copilot":
		totalPrompt = int64(g.between(500, 2800))
		freshMin, freshMax = 0.45, 0.65
		outMin, outMax = 60, 400
		creaMin, creaMax = 0, 400
		webPct = 0
	}

	// Long-context turns: rare, only on models that price the surcharge.
	if s.kind == "coding" && p.LongContextThreshold > 0 && g.rng.Intn(1000) < 3 {
		totalPrompt = p.LongContextThreshold + int64(g.between(20000, 60000))
		freshMin, freshMax = 0.25, 0.42
		creaMin, creaMax = 4000, 18000
	}

	fresh := int64(g.between(freshMin, freshMax) * float64(totalPrompt))
	cacheRead := totalPrompt - fresh
	if cacheRead < 0 {
		cacheRead = 0
	}
	t.input = fresh
	t.cacheRead = cacheRead
	t.cacheCrea = int64(g.between(float64(creaMin), float64(creaMax)))
	if p.CacheCreation1h > 0 && t.cacheCrea > 0 {
		t.cache1h = int64(0.2 * float64(t.cacheCrea))
	}
	t.output = int64(g.between(float64(outMin), float64(outMax)))
	if strings.Contains(model, "deepseek-v4-pro") || model == "grok-4.5" || strings.Contains(model, "kimi-k3") {
		t.reasoning = int64(g.between(200, 2500))
	}
	if webPct > 0 && g.rng.Intn(100) < webPct {
		t.webSearch = int64(1 + g.rng.Intn(2))
	}
	if p.FastMultiplier > 0 && g.rng.Intn(100) < 5 {
		t.fast = true
	}
	t.ttft = 150 + g.rng.Int63n(800)
	t.totalMS = 800 + t.output*8 + g.rng.Int63n(3000)

	// Estimate what every surface will reprice this turn at load time
	// (recorded costs are left 0 by design). Used only for the CLI summary.
	if costP, ok := g.engine.Lookup(t.model); ok {
		g.res.EstimatedUSD += cost.Compute(costP, cost.TokenBundle{
			Input: t.input, Output: t.output, CacheRead: t.cacheRead,
			CacheCreation: t.cacheCrea, CacheCreation1h: t.cache1h,
			Reasoning: t.reasoning, WebSearchRequests: t.webSearch, Fast: t.fast,
		})
	}

	if s.kind == "coding" || s.harness.tool == models.ToolCopilot {
		act := g.makeAction(s, &t, i)
		if act != nil {
			t.action = act
		}
	}
	return t
}

var seedPaths = []string{
	"internal/api/handlers.go", "internal/store/store.go", "internal/models/models.go",
	"web/src/pages/Overview.tsx", "cmd/observer/main.go", "internal/watcher/watcher.go",
	"internal/intelligence/cost/pricing.go", "web/src/api/client.ts",
	"internal/orgserver/server.go", "Makefile", "web/src/components/Chart.tsx",
	"internal/db/migrations/003_actions.sql",
}

var seedCommands = []string{
	"go test ./internal/store/", "make build", "go vet ./...", "npm run build",
	"go test -race ./internal/watcher/", "make lint", "go build ./cmd/observer",
	"node scripts/check.mjs", "go test ./internal/intelligence/dashboard/ -count=1",
}

func (g *gen) pickActionType(tool string) string {
	if tool == models.ToolCopilot {
		return "accept_suggestion"
	}
	r := g.rng.Intn(100)
	switch {
	case r < 20:
		return "run_command"
	case r < 40:
		return "write_file"
	case r < 65:
		return "edit_file"
	case r < 87:
		return "read_file"
	case r < 93:
		return "grep"
	case r < 97:
		return "view"
	default:
		return "ls"
	}
}

func (g *gen) makeAction(s *sessionInfo, t *turnInfo, i int) *models.ToolEvent {
	typ := g.pickActionType(s.tool)
	target := seedPaths[g.rng.Intn(len(seedPaths))]
	if typ == "run_command" {
		target = seedCommands[g.rng.Intn(len(seedCommands))]
	}
	success := true
	errMsg := ""
	if typ == "run_command" && g.rng.Intn(100) < 12 {
		success = false
		errMsg = "exit status 1: command failed with non-zero exit code"
	}
	ev := &models.ToolEvent{
		SourceFile:    seedSourceFilePF + s.id + "/actions",
		SourceEventID: "act-" + s.id + "-" + strconv.Itoa(i),
		SessionID:     s.id,
		ProjectRoot:   s.project,
		Timestamp:     t.ts,
		TurnIndex:     i + 1,
		GitBranch:     seedBranch,
		Model:         t.model,
		Tool:          s.tool,
		ActionType:    typ,
		Target:        target,
		Success:       success,
		ErrorMessage:  errMsg,
		DurationMs:    250 + g.rng.Int63n(9000),
		RawToolName:   typ,
	}
	if typ == "write_file" || typ == "edit_file" {
		ev.ContentBytes = 120 + g.rng.Int63n(3800)
	}
	if g.rng.Intn(100) < 4 {
		ev.IsSidechain = true
	}
	return ev
}

// writeSession persists one session (FK order: project → session → turns →
// actions) plus, when withRouter is set, per-session routing advice.
func (g *gen) writeSession(ctx context.Context, s *sessionInfo, withRouter bool) error {
	pid := g.projIDs[s.project]
	sess := models.Session{
		ID:           s.id,
		ProjectID:    pid,
		Tool:         s.tool,
		Model:        s.turns[0].model,
		GitBranch:    seedBranch,
		StartedAt:    s.start,
		EndedAt:      s.end,
		TotalActions: 0,
		Metadata:     "{}",
		UserEmail:    s.user.email,
	}
	for _, t := range s.turns {
		if t.action != nil {
			sess.TotalActions++
		}
	}
	if err := g.st.UpsertSession(ctx, sess); err != nil {
		return fmt.Errorf("upsert session %s: %w", s.id, err)
	}
	g.res.Sessions++

	var tokens []models.TokenEvent
	for i, t := range s.turns {
		if t.http > 0 {
			if _, err := g.st.InsertAPITurn(ctx, models.APITurn{
				SessionID:       s.id,
				ProjectID:       pid,
				Timestamp:       t.ts,
				Provider:        providerFor(t.model),
				Model:           "",
				RequestID:       "seedreq-" + s.id + "-" + strconv.Itoa(i),
				HTTPStatus:      t.http,
				ErrorClass:      t.errClass,
				ErrorMessage:    t.errMsg,
				TotalResponseMS: t.totalMS,
				UserEmail:       s.user.email,
				Source:          seedSource,
			}); err != nil {
				return fmt.Errorf("insert api turn (err) %s: %w", s.id, err)
			}
			g.res.APITurns++
			continue
		}
		if s.channel == "proxy" {
			if _, err := g.st.InsertAPITurn(ctx, models.APITurn{
				SessionID:             s.id,
				ProjectID:             pid,
				Timestamp:             t.ts,
				Provider:              providerFor(t.model),
				Model:                 t.model,
				RequestID:             "seedreq-" + s.id + "-" + strconv.Itoa(i),
				InputTokens:           t.input,
				OutputTokens:          t.output,
				CacheReadTokens:       t.cacheRead,
				CacheCreationTokens:   t.cacheCrea,
				CacheCreation1hTokens: t.cache1h,
				WebSearchRequests:     t.webSearch,
				Fast:                  t.fast,
				MessageCount:          t.msgCount,
				ToolUseCount:          t.toolUse,
				TimeToFirstTokenMS:    t.ttft,
				TotalResponseMS:       t.totalMS,
				StopReason:            t.stop,
				UserEmail:             s.user.email,
				Source:                seedSource,
			}); err != nil {
				return fmt.Errorf("insert api turn %s: %w", s.id, err)
			}
			g.res.APITurns++
		} else {
			tokens = append(tokens, models.TokenEvent{
				SourceFile:            seedSourceFilePF + s.id + "/tokens",
				SourceEventID:         "tok-" + s.id + "-" + strconv.Itoa(i),
				SessionID:             s.id,
				ProjectRoot:           s.project,
				GitBranch:             seedBranch,
				Timestamp:             t.ts,
				Tool:                  s.tool,
				Model:                 t.model,
				InputTokens:           t.input,
				OutputTokens:          t.output,
				CacheReadTokens:       t.cacheRead,
				CacheCreationTokens:   t.cacheCrea,
				CacheCreation1hTokens: t.cache1h,
				ReasoningTokens:       t.reasoning,
				WebSearchRequests:     t.webSearch,
				Fast:                  t.fast,
				EstimatedCostUSD:      0,
				Source:                seedSource,
				Reliability:           "estimated",
				MessageID:             "seedmsg-" + s.id + "-" + strconv.Itoa(i),
				TurnID:                "seedturn-" + s.id + "-" + strconv.Itoa(i),
				UserEmail:             s.user.email,
			})
			g.res.TokenEvents++
		}
	}
	if len(tokens) > 0 {
		if _, err := g.st.InsertTokenEvents(ctx, tokens); err != nil {
			return fmt.Errorf("insert token events %s: %w", s.id, err)
		}
	}

	var events []models.ToolEvent
	for _, t := range s.turns {
		if t.action != nil {
			events = append(events, *t.action)
		}
	}
	if len(events) > 0 {
		if _, err := g.st.Ingest(ctx, events, nil, store.IngestOptions{}); err != nil {
			return fmt.Errorf("ingest actions %s: %w", s.id, err)
		}
		g.res.Actions += len(events)
	}

	if withRouter {
		for _, t := range s.turns {
			if t.http > 0 || t.action == nil || !routerCandidate(t.model) {
				continue
			}
			if g.rng.Intn(100) < 25 {
				g.insertRouterDecision(ctx, s, t, t.ts)
			}
		}
	}
	return nil
}

// insertRouterDecision writes one advise-mode router_decisions row.
func (g *gen) insertRouterDecision(ctx context.Context, s *sessionInfo, t turnInfo, ts time.Time) {
	selected := "gpt-5-mini"
	if t.model == selected {
		selected = "gpt-5.4-mini"
	}
	savings := g.between(0.02, 0.25)
	g.res.RouterRows++
	_, _ = g.db.ExecContext(
		ctx, `INSERT INTO router_decisions
		(session_id, ts, mode, channel, original_model, selected_model, turn_kind,
		 policy_name, policy_hash, reason_codes, est_savings_usd, cache_forfeit_usd,
		 estimate_version, applied)
		VALUES (?, ?, 'advise', 'B', ?, ?, 'coding', ?, ?, ?, ?, 0, 'seed-1', 0)`,
		s.id,
		ts.UTC().Format(time.RFC3339Nano),
		t.model,
		selected,
		seedPolicy,
		seedRoutePolicy,
		`["cost_savings"]`,
		math.Round(savings*100)/100,
	)
}

// seedShadowWindow guarantees advise evidence inside the rolling 24h window
// AdviseShadowSignal reads, regardless of what time of day the seed runs.
func (g *gen) seedShadowWindow(ctx context.Context) error {
	now := time.Now().UTC()
	var candidates []*sessionInfo
	// Reuse yesterday's coding sessions as hosts for the synthetic advice.
	// The generator state isn't retained across days, so we re-derive a
	// small set of plausible session ids deterministically.
	for u := 0; u < len(g.users) && u < 8; u++ {
		for k := 0; k < 6; k++ {
			s := &sessionInfo{
				id:   fmt.Sprintf("seed-%s-%s-%d", strings.ToLower(g.users[u].name), now.Add(-24*time.Hour).Format("20060102"), k),
				user: g.users[u],
			}
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	for i := 0; i < 60; i++ {
		ts := now.Add(-(time.Duration(20+18*i) * time.Minute))
		if ts.Before(now.Add(-24 * time.Hour)) {
			break
		}
		host := candidates[i%len(candidates)]
		model := []string{"claude-opus-4-8", "claude-opus-5", "claude-sonnet-4-6", "gemini-3.1-pro-preview", "gpt-5.5", "grok-4.5"}[i%6]
		g.insertRouterDecision(ctx, host, turnInfo{model: model}, ts)
	}
	return nil
}

func routerCandidate(model string) bool {
	switch model {
	case "claude-opus-4-8", "claude-opus-5", "claude-sonnet-4-6", "claude-sonnet-4-20250514",
		"gemini-2.5-pro", "gemini-3.1-pro-preview", "gpt-5.2", "gpt-5.5", "grok-4.5", "kimi-k3":
		return true
	}
	return false
}

func providerFor(m string) string {
	switch {
	case strings.HasPrefix(m, "claude"):
		return "anthropic"
	case strings.HasPrefix(m, "gpt-"), strings.HasPrefix(m, "o4"), strings.HasPrefix(m, "o3"), strings.HasPrefix(m, "gpt-oss"):
		return "openai"
	case strings.HasPrefix(m, "gemini"):
		return "google"
	case strings.HasPrefix(m, "grok"):
		return "xai"
	case strings.HasPrefix(m, "deepseek"):
		return "deepseek"
	case strings.HasPrefix(m, "kimi"):
		return "moonshot"
	case strings.HasPrefix(m, "qwen"):
		return "qwen"
	case strings.HasPrefix(m, "glm"):
		return "glm"
	case strings.HasPrefix(m, "mistral"):
		return "mistral"
	case strings.HasPrefix(m, "minimax"):
		return "minimax"
	}
	return "other"
}
