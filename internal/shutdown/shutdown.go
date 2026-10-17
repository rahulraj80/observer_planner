package shutdown

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Tracker records which named components are running and the current stop
// stage. The zero value is not usable; build one with NewTracker. Safe for
// concurrent use.
type Tracker struct {
	mu      sync.Mutex
	running map[string]int
	stage   string
}

// NewTracker returns an empty Tracker in the "running" stage.
func NewTracker() *Tracker {
	return &Tracker{running: map[string]int{}, stage: StageRunning}
}

// Stop stages, in the order a daemon passes through them.
const (
	// StageRunning is the stage before any stop was requested.
	StageRunning = "running"
	// StageComponents is waiting for the named components to return.
	StageComponents = "components"
	// StageCheckpoint is the final WAL checkpoint after every component
	// returned.
	StageCheckpoint = "checkpoint"
	// StageClosing is the deferred cleanups (terminal stack, DB handles,
	// lock file) after the checkpoint.
	StageClosing = "closing"
)

// Start marks one instance of name as running and returns the func that
// marks it finished. Call the returned func exactly once (a second call is a
// no-op).
func (t *Tracker) Start(name string) (done func()) {
	t.mu.Lock()
	t.running[name]++
	t.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			t.mu.Lock()
			if t.running[name] <= 1 {
				delete(t.running, name)
			} else {
				t.running[name]--
			}
			t.mu.Unlock()
		})
	}
}

// Running returns the names still running, sorted. A name running more
// than once is listed as "name x<count>".
func (t *Tracker) Running() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, len(t.running))
	for name, n := range t.running {
		if n > 1 {
			out = append(out, fmt.Sprintf("%s x%d", name, n))
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// SetStage records the current stop stage.
func (t *Tracker) SetStage(stage string) {
	t.mu.Lock()
	t.stage = stage
	t.mu.Unlock()
}

// AdvanceStage moves the stage from `from` to `to` and reports whether it
// did; a stage already past `from` is left alone (the stop's first observer
// does not rewind a later stage the main path already set).
func (t *Tracker) AdvanceStage(from, to string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stage != from {
		return false
	}
	t.stage = to
	return true
}

// Stage returns the current stop stage.
func (t *Tracker) Stage() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stage
}

// Report is what the watchdog hands its callbacks: how long the stop has
// taken against its bound, the stage it is in, and what is still running.
type Report struct {
	Bound   time.Duration
	Elapsed time.Duration
	Stage   string
	Running []string
}

// Blocking names what is holding the stop up: the running components when
// any are left, else the stage itself (a cleanup after every component
// returned).
func (r Report) Blocking() string {
	if len(r.Running) > 0 {
		return strings.Join(r.Running, ", ")
	}
	return "stage " + r.Stage
}

// Watchdog times one stop. Arm starts the clock (idempotent), Disarm ends it
// (idempotent; an Arm after Disarm does nothing). Part-way through the bound
// it calls Progress once if anything is still running; at the bound it calls
// Overrun once. Both callbacks run on the watchdog's own goroutine.
type Watchdog struct {
	tracker  *Tracker
	bound    time.Duration
	progress time.Duration
	onProg   func(Report)
	onOver   func(Report)

	mu       sync.Mutex
	armed    bool
	disarmed bool
	stop     chan struct{}
	started  time.Time
}

// Options configures a Watchdog.
type Options struct {
	// Bound is the longest a stop may take before Overrun fires. Must be
	// positive.
	Bound time.Duration
	// ProgressAfter is when Progress fires; zero means a quarter of Bound.
	ProgressAfter time.Duration
	// Progress, when non-nil, is called once at ProgressAfter if the stop
	// has not finished (Disarm not yet called).
	Progress func(Report)
	// Overrun is called once at Bound if the stop has not finished.
	Overrun func(Report)
}

// NewWatchdog returns an unarmed watchdog over tracker.
func NewWatchdog(tracker *Tracker, opts Options) *Watchdog {
	prog := opts.ProgressAfter
	if prog <= 0 || prog >= opts.Bound {
		prog = opts.Bound / 4
	}
	return &Watchdog{
		tracker:  tracker,
		bound:    opts.Bound,
		progress: prog,
		onProg:   opts.Progress,
		onOver:   opts.Overrun,
		stop:     make(chan struct{}),
	}
}

// Arm starts the stop clock. Only the first call (before Disarm) has any
// effect.
func (w *Watchdog) Arm() {
	w.mu.Lock()
	if w.armed || w.disarmed || w.bound <= 0 {
		w.mu.Unlock()
		return
	}
	w.armed = true
	w.started = time.Now()
	w.mu.Unlock()
	go w.run()
}

// Disarm ends the stop clock: the process is about to exit on its own.
func (w *Watchdog) Disarm() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.disarmed {
		return
	}
	w.disarmed = true
	close(w.stop)
}

// Elapsed reports how long the stop has run (zero before Arm).
func (w *Watchdog) Elapsed() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.armed {
		return 0
	}
	return time.Since(w.started)
}

func (w *Watchdog) report() Report {
	return Report{
		Bound:   w.bound,
		Elapsed: w.Elapsed(),
		Stage:   w.tracker.Stage(),
		Running: w.tracker.Running(),
	}
}

func (w *Watchdog) run() {
	prog := time.NewTimer(w.progress)
	defer prog.Stop()
	over := time.NewTimer(w.bound)
	defer over.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-prog.C:
			if w.onProg != nil {
				w.onProg(w.report())
			}
		case <-over.C:
			if w.onOver != nil {
				w.onOver(w.report())
			}
			return
		}
	}
}
