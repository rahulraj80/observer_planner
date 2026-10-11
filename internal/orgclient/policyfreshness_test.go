package orgclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/policyfresh"
)

// probeServer serves the policy-version probe from a mutable version vector
// and counts every OTHER request by path (the rail fetches a dispatch makes).
type probeServer struct {
	mu        sync.Mutex
	srv       *httptest.Server
	rails     map[string]int64
	notFound  bool
	probes    int
	gotIfNone []string
	railHits  map[string]int
	// holds makes the fake a holding server (T-474): a probe that asks for a
	// hold with a matching If-None-Match stays open until set() moves the
	// vector or the hold ends, and every held answer carries the header.
	holds bool
	moved chan struct{}
	// arrivals counts probe requests when they ARRIVE (a held probe is in
	// probes only once answered).
	arrivals int
}

func newProbeServer(t *testing.T) *probeServer {
	t.Helper()
	ps := &probeServer{railHits: map[string]int{}, moved: make(chan struct{})}
	ps.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ps.hold(w, r)
		ps.mu.Lock()
		defer ps.mu.Unlock()
		if r.URL.Path != orgcontract.PolicyVersionsPath {
			ps.railHits[r.URL.Path]++
			// No rail is served: the test only asserts WHICH rails the
			// checker asked for, never that a hint applied anything.
			http.NotFound(w, r)
			return
		}
		ps.probes++
		ps.gotIfNone = append(ps.gotIfNone, r.Header.Get("If-None-Match"))
		if ps.notFound {
			http.NotFound(w, r)
			return
		}
		etag := `"pv-test-` + versionKey(ps.rails) + `"`
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		writeTestJSON(w, http.StatusOK, orgcontract.PolicyVersionsDoc{RailVersions: ps.rails})
	}))
	t.Cleanup(ps.srv.Close)
	return ps
}

func versionKey(m map[string]int64) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for _, k := range keys {
		out += k + "=" + strconv.FormatInt(m[k], 10) + ";"
	}
	return out
}

func (ps *probeServer) set(rails map[string]int64) {
	ps.mu.Lock()
	ps.rails = rails
	close(ps.moved)
	ps.moved = make(chan struct{})
	ps.mu.Unlock()
}

// hold implements the holding half of the fake: it returns once the probe
// may be answered (the vector moved, the hold ended, or the probe is not a
// held one).
func (ps *probeServer) hold(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != orgcontract.PolicyVersionsPath {
		return
	}
	ps.mu.Lock()
	ps.arrivals++
	ps.mu.Unlock()
	wait, _ := strconv.Atoi(r.URL.Query().Get(orgcontract.PolicyVersionsHoldParam))
	ps.mu.Lock()
	holds := ps.holds
	ps.mu.Unlock()
	if !holds || wait <= 0 {
		return
	}
	w.Header().Set(orgcontract.HeaderPolicyVersionsHold, strconv.Itoa(wait))
	deadline := time.After(time.Duration(wait) * time.Second)
	for {
		ps.mu.Lock()
		etag := `"pv-test-` + versionKey(ps.rails) + `"`
		moved := ps.moved
		ps.mu.Unlock()
		if r.Header.Get("If-None-Match") != etag {
			return
		}
		select {
		case <-moved:
		case <-deadline:
			return
		case <-r.Context().Done():
			return
		}
	}
}

// count reports the probe requests that arrived and the budget rail fetches.
func (ps *probeServer) count() (arrivals int, budgetHits int) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.arrivals, ps.railHits["/api/agent/budget"]
}

func (ps *probeServer) totalRailHits() int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	n := 0
	for _, v := range ps.railHits {
		n += v
	}
	return n
}

func drained(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// TestPolicyVersionProbe walks the probe ladder: the first answer is a
// baseline (nothing fetched), an unchanged vector is a 304 that fetches
// nothing, a moved budget rail fetches the budget+pricing rails, a moved
// bundle only wakes the bundle poll, and a 404 silences the checker.
func TestPolicyVersionProbe(t *testing.T) {
	ps := newProbeServer(t)
	base := map[string]int64{
		orgcontract.RailBudget: 1, orgcontract.RailPricing: 1, orgcontract.RailBundle: 1,
		orgcontract.RailRouting: 1, orgcontract.RailAnnouncement: 1,
	}
	ps.set(base)
	c, _, _ := enrolledClient(t, ps.srv.URL)
	ctx := context.Background()
	now := time.Now()

	// 1. First observation: baseline only.
	c.probePolicyVersions(ctx, now, 0)
	if got := ps.totalRailHits(); got != 0 {
		t.Fatalf("first probe fetched %d rails, want 0 (baseline only)", got)
	}
	if c.fresh.st.InFlight {
		t.Fatal("InFlight left set after the probe returned")
	}

	// 2. Unchanged: the probe rides If-None-Match into a 304.
	c.probePolicyVersions(ctx, now, 0)
	if ps.gotIfNone[1] == "" {
		t.Fatal("second probe sent no If-None-Match")
	}
	if got := ps.totalRailHits(); got != 0 {
		t.Fatalf("304 probe fetched %d rails, want 0", got)
	}

	// 3. Budget moved: the budget AND pricing rails are fetched (one
	// governance fact), nothing else; the bundle poll is not woken.
	moved := copyVersions(base)
	moved[orgcontract.RailBudget] = 2
	ps.set(moved)
	c.probePolicyVersions(ctx, now, 0)
	if got := ps.totalRailHits(); got == 0 {
		t.Fatal("budget rail moved but nothing was fetched")
	}
	ps.mu.Lock()
	for path := range ps.railHits {
		if path == orgcontract.PolicyVersionsPath {
			t.Fatalf("probe path counted as a rail: %s", path)
		}
	}
	ps.mu.Unlock()
	if drained(c.policyBundleKick) {
		t.Fatal("bundle poll woken for a budget-only change")
	}

	// 4. Bundle moved: no rail fetch from the checker itself - it wakes
	// PolicyPollLoop, whose signed fetch is unchanged.
	before := ps.totalRailHits()
	moved2 := copyVersions(moved)
	moved2[orgcontract.RailBundle] = 2
	ps.set(moved2)
	c.probePolicyVersions(ctx, now, 0)
	if got := ps.totalRailHits(); got != before {
		t.Fatalf("bundle change fetched %d rails directly, want 0", got-before)
	}
	if !drained(c.policyBundleKick) {
		t.Fatal("bundle change did not wake the bundle poll")
	}

	// 5. 404: the checker goes quiet for the unsupported backoff, once.
	ps.mu.Lock()
	ps.notFound = true
	ps.mu.Unlock()
	c.probePolicyVersions(ctx, now, 0)
	if until := c.fresh.st.UnsupportedUntil; !until.After(now.Add(30 * time.Minute)) {
		t.Fatalf("UnsupportedUntil = %v, want ~1h after %v", until, now)
	}
	if !c.fresh.loggedUnsupported {
		t.Fatal("404 not recorded")
	}
}

// TestRecordRailVersionsQueue pins the ack path: a rail the push cycle
// refreshes itself is not re-queued, the bundle always is, and an ack outside
// a rail-refreshing cycle queues every moved rail.
func TestRecordRailVersionsQueue(t *testing.T) {
	tests := []struct {
		name    string
		covered bool
		prev    map[string]int64
		next    map[string]int64
		want    []string
	}{
		{
			"first ack is a baseline", true, nil,
			map[string]int64{"budget": 1, "bundle": 1},
			nil,
		},
		{
			"unchanged queues nothing", true,
			map[string]int64{"budget": 1, "bundle": 1},
			map[string]int64{"budget": 1, "bundle": 1},
			nil,
		},
		{
			"covered budget move is the cycle's own", true,
			map[string]int64{"budget": 1, "bundle": 1},
			map[string]int64{"budget": 2, "bundle": 1},
			nil,
		},
		{
			"bundle move is always queued", true,
			map[string]int64{"budget": 1, "bundle": 1},
			map[string]int64{"budget": 2, "bundle": 2},
			[]string{"bundle"},
		},
		{
			"uncovered cycle queues every move", false,
			map[string]int64{"budget": 1, "routing": 1, "bundle": 1},
			map[string]int64{"budget": 2, "routing": 2, "bundle": 1},
			[]string{"budget", "routing"},
		},
		{
			"covered display move is the cycle's own", true,
			map[string]int64{"display": 1, "bundle": 1},
			map[string]int64{"display": 2, "bundle": 1},
			nil,
		},
		{
			"light cycle queues a display move", false,
			map[string]int64{"display": 1, "bundle": 1},
			map[string]int64{"display": 2, "bundle": 1},
			[]string{"display"},
		},
		{
			"a version going backwards is not a move", false,
			map[string]int64{"budget": 5},
			map[string]int64{"budget": 4},
			nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := enrolledClient(t, "http://127.0.0.1:1")
			c.fresh.lastRail = copyVersions(tc.prev)
			c.cycleRails.Store(tc.covered)
			c.recordRailVersions(tc.next)
			got := c.takePendingRails()
			sort.Strings(got)
			if len(got) != len(tc.want) {
				t.Fatalf("queued %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("queued %v, want %v", got, tc.want)
				}
			}
			if kicked := drained(c.freshDispatch); kicked != (len(tc.want) > 0) {
				t.Fatalf("dispatch kick = %v, want %v", kicked, len(tc.want) > 0)
			}
			if versionKey(c.fresh.lastRail) != versionKey(tc.next) {
				t.Fatalf("baseline = %v, want %v", c.fresh.lastRail, tc.next)
			}
		})
	}
}

// TestPolicyFreshnessLoopProbesOnActivity runs the real loop: activity after
// a long idle probes at once; a burst inside the min-interval does not probe
// again.
func TestPolicyFreshnessLoopProbesOnActivity(t *testing.T) {
	ps := newProbeServer(t)
	ps.set(map[string]int64{orgcontract.RailBudget: 1})
	c, _, _ := enrolledClient(t, ps.srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.PolicyFreshnessLoop(ctx) }()

	c.NoteActivity()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ps.mu.Lock()
		n := ps.probes
		ps.mu.Unlock()
		if n >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("activity after idle did not probe")
		}
		time.Sleep(20 * time.Millisecond)
	}
	for i := 0; i < 20; i++ {
		c.NoteActivity()
	}
	time.Sleep(300 * time.Millisecond)
	ps.mu.Lock()
	n := ps.probes
	ps.mu.Unlock()
	if n != 1 {
		t.Fatalf("a burst inside the min-interval probed %d times, want 1 total", n)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not stop on cancel")
	}
}

// TestPolicyFreshnessLoopHeldProbe is the T-474 node half against a holding
// server: an active node keeps one held probe open, so a budget change is
// fetched within about a second of the edit (not at the next 30 s floor), and
// it does so without a probe storm: while nothing changes the node sends no
// new probe for the length of the hold.
func TestPolicyFreshnessLoopHeldProbe(t *testing.T) {
	ps := newProbeServer(t)
	ps.holds = true
	ps.set(map[string]int64{orgcontract.RailBudget: 1})
	c, _, _ := enrolledClient(t, ps.srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.PolicyFreshnessLoop(ctx) }()
	// An active developer: activity every 100 ms.
	go func() {
		tk := time.NewTicker(100 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				c.NoteActivity()
			}
		}
	}()
	waitFor := func(what string, d time.Duration, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(d)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("%s: not within %v", what, d)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	// The baseline probe, then (one HoldGap later) the held probe.
	waitFor("held probe open", policyfresh.DefaultHoldGap+3*time.Second, func() bool {
		n, _ := ps.count()
		return n >= 2
	})
	time.Sleep(300 * time.Millisecond)
	probesBefore, _ := ps.count()

	edit := time.Now()
	ps.set(map[string]int64{orgcontract.RailBudget: 2})
	waitFor("budget fetched after the edit", 3*time.Second, func() bool {
		_, hits := ps.count()
		return hits > 0
	})
	pickup := time.Since(edit)
	t.Logf("active node picked up the budget edit in %v (held probe)", pickup)
	if pickup > 2*time.Second {
		t.Fatalf("pickup %v, want within about a second", pickup)
	}
	if n, _ := ps.count(); n != probesBefore {
		t.Fatalf("the change was answered by %d new probes, want the held one (0 new)", n-probesBefore)
	}
	// While nothing changes the re-armed held probe sits open: no storm.
	time.Sleep(2 * time.Second)
	if n, _ := ps.count(); n > probesBefore+1 {
		t.Fatalf("%d probes in 2 s of quiet activity, want at most the one re-armed held probe", n-probesBefore)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not stop on cancel (a held probe must not outlive its context)")
	}
}

// TestMaybeKickRailVersionsFromAck checks the ack decode feeds the queue.
func TestMaybeKickRailVersionsFromAck(t *testing.T) {
	c, _, _ := enrolledClient(t, "http://127.0.0.1:1")
	c.maybeKickPolicyResourceFetch([]byte(`{"rail_versions":{"bundle":1}}`))
	c.maybeKickPolicyResourceFetch([]byte(`{"rail_versions":{"bundle":2}}`))
	if got := c.takePendingRails(); len(got) != 1 || got[0] != "bundle" {
		t.Fatalf("pending = %v, want [bundle]", got)
	}
	// A policy_versions-only ack (an older server) leaves the rail
	// baseline untouched.
	c.maybeKickPolicyResourceFetch([]byte(`{"policy_versions":{"admission.input":3}}`))
	if c.fresh.lastRail["bundle"] != 2 {
		t.Fatalf("baseline = %v, want bundle=2 kept", c.fresh.lastRail)
	}
}
