package feedstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/fsatomic"
	"github.com/marmutapp/superbased-observer/internal/notify/feed"
)

// Filename is the state file's name beside the node database.
const Filename = "notifications-state.json"

// Schema is the file's schema version.
const Schema = 1

// Bounds.
const (
	Retention  = 90 * 24 * time.Hour
	MaxEntries = 5000
	maxBytes   = 2 << 20
)

// Action is a state change a viewer asks for.
type Action string

// The actions.
const (
	ActionRead    Action = "read"
	ActionDismiss Action = "dismiss"
)

// ErrUnknownAction is returned for an action outside the closed set.
var ErrUnknownAction = errors.New("feedstate: unknown action")

type entry struct {
	Read      bool      `json:"read,omitempty"`
	Dismissed bool      `json:"dismissed,omitempty"`
	At        time.Time `json:"at"`
}

type file struct {
	Schema int              `json:"schema"`
	States map[string]entry `json:"states"`
}

// Path returns the state file beside dbPath ("" when dbPath is empty).
func Path(dbPath string) string {
	if dbPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(dbPath), Filename)
}

// Store serializes this process's reads and writes of one state file.
type Store struct {
	path string
	mu   sync.Mutex
}

// New returns the store for path. An empty path keeps nothing (Load returns
// no state, Mark is a no-op), which is what a dashboard with no database path
// (tests, demo) wants.
func New(path string) *Store { return &Store{path: path} }

// Load returns the persisted state by notification id. A missing, oversized
// or unreadable file is "no state yet" (every notification unread), never an
// error the bell would have to explain.
func (s *Store) Load() map[string]feed.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return toStates(s.read())
}

// Mark applies action to ids at now and persists the result. Dismiss implies
// read.
func (s *Store) Mark(ids []string, action Action, now time.Time) error {
	if action != ActionRead && action != ActionDismiss {
		return fmt.Errorf("%w: %q", ErrUnknownAction, action)
	}
	if s.path == "" || len(ids) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.read()
	for _, id := range ids {
		if id == "" {
			continue
		}
		e := f.States[id]
		e.Read = true
		if action == ActionDismiss {
			e.Dismissed = true
		}
		e.At = now.UTC()
		f.States[id] = e
	}
	prune(f.States, now)
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("feedstate.Mark: %w", err)
	}
	if err := fsatomic.WriteFile(s.path, data, fsatomic.Options{TempPattern: ".notifications-*.tmp"}); err != nil {
		return fmt.Errorf("feedstate.Mark: %w", err)
	}
	return nil
}

func (s *Store) read() file {
	out := file{Schema: Schema, States: map[string]entry{}}
	if s.path == "" {
		return out
	}
	fh, err := os.Open(s.path)
	if err != nil {
		return out
	}
	defer func() { _ = fh.Close() }()
	data, err := io.ReadAll(io.LimitReader(fh, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return out
	}
	var f file
	if json.Unmarshal(data, &f) != nil || f.Schema != Schema || f.States == nil {
		return out
	}
	return f
}

// prune drops entries older than Retention, then the oldest beyond
// MaxEntries.
func prune(states map[string]entry, now time.Time) {
	for id, e := range states {
		if now.Sub(e.At) > Retention {
			delete(states, id)
		}
	}
	if len(states) <= MaxEntries {
		return
	}
	ids := make([]string, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return states[ids[i]].At.Before(states[ids[j]].At) })
	for _, id := range ids[:len(ids)-MaxEntries] {
		delete(states, id)
	}
}

func toStates(f file) map[string]feed.State {
	out := make(map[string]feed.State, len(f.States))
	for id, e := range f.States {
		out[id] = feed.State{Read: e.Read, Dismissed: e.Dismissed}
	}
	return out
}
