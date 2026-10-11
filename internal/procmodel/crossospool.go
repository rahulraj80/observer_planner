package procmodel

import "time"

// CrossOSPool is the key set of one full cross-OS pass for one session.
type CrossOSPool struct {
	keys    map[string]struct{}
	parents map[string]struct{}
}

// NewCrossOSPool records the candidate pool a full pass loaded.
func NewCrossOSPool(runs []CrossOSProcRef) CrossOSPool {
	p := CrossOSPool{
		keys:    make(map[string]struct{}, len(runs)),
		parents: make(map[string]struct{}),
	}
	for i := range runs {
		p.keys[runs[i].ProcessKey] = struct{}{}
		if runs[i].ParentProcessKey != "" {
			p.parents[runs[i].ParentProcessKey] = struct{}{}
		}
	}
	return p
}

// Len is the number of process rows in the pool.
func (p CrossOSPool) Len() int { return len(p.keys) }

// CrossOSDelta is one process row captured since the last sweep: just the
// columns that decide whether it can enter a session's cross-OS pool.
type CrossOSDelta struct {
	ProcessKey       string
	ParentProcessKey string
	SessionID        string
	StartedAt        time.Time
}

// crossOSPoolRule is one way a new row can change a session's cross-OS input.
type crossOSPoolRule struct {
	name    string
	matches func(p CrossOSPool, sess CrossOSSessionRef, d CrossOSDelta, window time.Duration) bool
}

// crossOSPoolRules is the ordered table Touched walks. Each row mirrors one
// arm of the store's candidate query (seeds, down-walk, up-walk).
var crossOSPoolRules = []crossOSPoolRule{
	{"already carries the session (seed)", func(_ CrossOSPool, sess CrossOSSessionRef, d CrossOSDelta, _ time.Duration) bool {
		return d.SessionID != "" && d.SessionID == sess.SessionID
	}},
	{"started inside the anchor window (seed)", func(_ CrossOSPool, sess CrossOSSessionRef, d CrossOSDelta, window time.Duration) bool {
		return !d.StartedAt.Before(sess.StartedAt.Add(-window)) && !d.StartedAt.After(sess.StartedAt.Add(window))
	}},
	{"child of a pooled row (down-walk)", func(p CrossOSPool, _ CrossOSSessionRef, d CrossOSDelta, _ time.Duration) bool {
		_, ok := p.keys[d.ParentProcessKey]
		return d.ParentProcessKey != "" && ok
	}},
	{"missing parent of a pooled row (up-walk)", func(p CrossOSPool, _ CrossOSSessionRef, d CrossOSDelta, _ time.Duration) bool {
		_, ok := p.parents[d.ProcessKey]
		return ok
	}},
}

// Touched reports whether any of the new rows can change the result of the
// session's cross-OS pass, i.e. whether a full pass must run. window is the
// candidate anchor window the store loads (±CrossOSWindow around the session
// start).
func (p CrossOSPool) Touched(sess CrossOSSessionRef, delta []CrossOSDelta, window time.Duration) bool {
	if window <= 0 {
		window = CrossOSWindow
	}
	for i := range delta {
		for _, rule := range crossOSPoolRules {
			if rule.matches(p, sess, delta[i], window) { //nolint:gosec // G602: the index is bounds-checked above
				return true
			}
		}
	}
	return false
}

// SameCrossOSSession reports whether two session refs describe the same
// cross-OS inputs (a session whose start, root or tool changed needs a full
// pass even with no new process rows).
func SameCrossOSSession(a, b CrossOSSessionRef) bool {
	return a.SessionID == b.SessionID && a.Tool == b.Tool && a.ProjectID == b.ProjectID &&
		a.ProjectRoot == b.ProjectRoot && a.StartedAt.Equal(b.StartedAt)
}
