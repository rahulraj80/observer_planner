package hookspool

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// Filename is the live spool's name beside the global config file.
const Filename = "hook-lifecycle.jsonl"

// asideMarker separates the live spool's name from an aside file's unix-nano
// stamp: hook-lifecycle.jsonl.aside-<unixnano>.
const asideMarker = ".aside-"

// MaxRecordBytes bounds one spooled line. A record past it is refused at
// append time (the hook logs and drops it) and skipped at drain time.
const MaxRecordBytes = 256 << 10

// Grace is how long an aside file waits before it is processed: far longer
// than one hook's open-then-append, so an append that raced the rename has
// landed in the aside file before the drainer reads it.
const Grace = 2 * time.Second

// version is the newest record schema version this package reads; a drainer
// skips a newer one. Version 1 is a tool-event row; version 2 adds the
// non-event kinds (KindCompaction), so a released drainer that knows only
// version 1 drops such a line as "newer schema" instead of ingesting it as
// an action with no action type.
const version = 2

// versionToolEvent is the version a plain tool-event row is written with,
// unchanged from the first release so every released drainer still reads it.
const versionToolEvent = 1

// KindCompaction marks a held compaction hook (T-320): a pre or post phase
// of a compact_lifecycle binding whose session the watcher had not ingested
// yet. The zero Kind is a tool-event row.
const KindCompaction = "compaction"

// recordVersion is the schema version a record is written with.
func recordVersion(r Record) int {
	if r.Kind != "" {
		return version
	}
	return versionToolEvent
}

// validRecord is the one shape check Append and the drain share: a tool and
// a session always, an action type for a tool-event row, a phase for a
// compaction row, and no kind this package does not know.
func validRecord(r Record) bool {
	if r.Tool == "" || r.SessionID == "" {
		return false
	}
	switch r.Kind {
	case "":
		return r.ActionType != ""
	case KindCompaction:
		return r.Phase != ""
	default:
		return false
	}
}

// Record is one spooled lifecycle row: the subset of models.ToolEvent a
// lifecycle hook produces. It is a plain value; FromEvent / Event convert.
type Record struct {
	V                  int       `json:"v"`
	Tool               string    `json:"tool"`
	SourceFile         string    `json:"source_file"`
	SourceEventID      string    `json:"source_event_id"`
	SessionID          string    `json:"session_id"`
	ProjectRoot        string    `json:"project_root,omitempty"`
	Timestamp          time.Time `json:"ts"`
	ActionType         string    `json:"action_type"`
	Target             string    `json:"target,omitempty"`
	MessageID          string    `json:"message_id,omitempty"`
	RawToolName        string    `json:"raw_tool_name,omitempty"`
	RawToolInput       string    `json:"raw_tool_input,omitempty"`
	ToolOutput         string    `json:"tool_output,omitempty"`
	PrecedingReasoning string    `json:"preceding_reasoning,omitempty"`
	IsSidechain        bool      `json:"is_sidechain,omitempty"`
	Success            bool      `json:"success"`
	// Metadata is the action metadata as JSON (models.ActionMetadata).
	Metadata *models.ActionMetadata `json:"metadata,omitempty"`
	// Kind is "" for a tool-event row, KindCompaction for a held compaction
	// hook. Phase and Trigger belong to a compaction row: the binding's phase
	// ("pre" captures, "post" reconciles) and the host's trigger text.
	Kind    string `json:"kind,omitempty"`
	Phase   string `json:"phase,omitempty"`
	Trigger string `json:"trigger,omitempty"`
	// Attempts counts the drain passes that held this record because its
	// session did not exist yet (T-320). It bounds the hold with MaxHoldAge.
	Attempts int `json:"attempts,omitempty"`
}

// FromEvent captures ev as a spool record.
func FromEvent(ev models.ToolEvent) Record {
	return Record{
		V:                  version,
		Tool:               ev.Tool,
		SourceFile:         ev.SourceFile,
		SourceEventID:      ev.SourceEventID,
		SessionID:          ev.SessionID,
		ProjectRoot:        ev.ProjectRoot,
		Timestamp:          ev.Timestamp.UTC(),
		ActionType:         ev.ActionType,
		Target:             ev.Target,
		MessageID:          ev.MessageID,
		RawToolName:        ev.RawToolName,
		RawToolInput:       ev.RawToolInput,
		ToolOutput:         ev.ToolOutput,
		PrecedingReasoning: ev.PrecedingReasoning,
		IsSidechain:        ev.IsSidechain,
		Success:            ev.Success,
		Metadata:           ev.Metadata,
	}
}

// Event rebuilds the models.ToolEvent the hook produced.
func (r Record) Event() models.ToolEvent {
	return models.ToolEvent{
		Tool:               r.Tool,
		SourceFile:         r.SourceFile,
		SourceEventID:      r.SourceEventID,
		SessionID:          r.SessionID,
		ProjectRoot:        r.ProjectRoot,
		Timestamp:          r.Timestamp,
		ActionType:         r.ActionType,
		Target:             r.Target,
		MessageID:          r.MessageID,
		RawToolName:        r.RawToolName,
		RawToolInput:       r.RawToolInput,
		ToolOutput:         r.ToolOutput,
		PrecedingReasoning: r.PrecedingReasoning,
		IsSidechain:        r.IsSidechain,
		Success:            r.Success,
		Metadata:           r.Metadata,
	}
}

// Path returns the live spool beside the resolved global config path ("" when
// configPath is empty).
func Path(configPath string) string {
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), Filename)
}

// Append adds one record with a single O_APPEND write. It never waits on the
// drainer and takes no lock: a failed append returns the error and the hook
// moves on (the row is lost, which the lifecycle ladder tolerates: the
// transcript still carries the session).
func Append(path string, r Record) error {
	if path == "" {
		return errors.New("hookspool.Append: empty path")
	}
	if !validRecord(r) {
		return errors.New("hookspool.Append: tool, session_id and action_type (or a known kind with its phase) are required")
	}
	r.V = recordVersion(r)
	if r.Timestamp.IsZero() {
		r.Timestamp = time.Now().UTC()
	}
	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("hookspool.Append: %w", err)
	}
	line = append(line, '\n')
	if len(line) > MaxRecordBytes {
		return errors.New("hookspool.Append: record too large")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("hookspool.Append: %w", err)
	}
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		return fmt.Errorf("hookspool.Append: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("hookspool.Append: %w", err)
	}
	return nil
}

// Aside is one set-aside spool file ready to be processed.
type Aside struct {
	Path string
	// Records are the decodable lines, in file order.
	Records []Record
	// Dropped counts lines that were malformed, oversized or from a newer
	// schema.
	Dropped int
}

// Ready returns the aside files set aside at least Grace before now, oldest
// first, decoded. A file that cannot be read is skipped (it is retried on the
// next pass).
func Ready(path string, now time.Time) ([]Aside, error) {
	if path == "" {
		return nil, nil
	}
	matches, err := filepath.Glob(path + asideMarker + "*")
	if err != nil {
		return nil, fmt.Errorf("hookspool.Ready: %w", err)
	}
	type stamped struct {
		path string
		at   int64
	}
	var files []stamped
	for _, m := range matches {
		n, perr := strconv.ParseInt(strings.TrimPrefix(m, path+asideMarker), 10, 64)
		if perr != nil {
			continue
		}
		if now.Sub(time.Unix(0, n)) < Grace {
			continue
		}
		files = append(files, stamped{m, n})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at < files[j].at })
	out := make([]Aside, 0, len(files))
	for _, f := range files {
		a, rerr := readAside(f.path)
		if rerr != nil {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

func readAside(p string) (Aside, error) {
	f, err := os.Open(p)
	if err != nil {
		return Aside{}, err
	}
	defer func() { _ = f.Close() }()
	a := Aside{Path: p}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), MaxRecordBytes+1)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r Record
		if json.Unmarshal([]byte(line), &r) != nil || r.V > version || !validRecord(r) {
			a.Dropped++
			continue
		}
		a.Records = append(a.Records, r)
	}
	if err := sc.Err(); err != nil {
		// An over-long line ends the scan; count it and keep what decoded.
		a.Dropped++
	}
	return a, nil
}

// HoldsKind reports whether any spool file (the live spool or an aside file,
// whatever its age) still holds a record of kind for sessionID. A recorder
// uses it to keep a session's records in order (P3-37): a later record of a
// session whose earlier record is still held must be held behind it, or the
// drainer would apply the earlier one after the later one. A file that cannot
// be read is skipped; an error is returned only when the spool cannot be
// listed.
func HoldsKind(path, kind, sessionID string) (bool, error) {
	if path == "" || sessionID == "" {
		return false, nil
	}
	matches, err := filepath.Glob(path + asideMarker + "*")
	if err != nil {
		return false, fmt.Errorf("hookspool.HoldsKind: %w", err)
	}
	for _, p := range append([]string{path}, matches...) {
		a, rerr := readAside(p)
		if rerr != nil {
			continue
		}
		for _, r := range a.Records {
			if r.Kind == kind && r.SessionID == sessionID {
				return true, nil
			}
		}
	}
	return false, nil
}

// Done removes a processed aside file.
func Done(a Aside) error {
	if err := os.Remove(a.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("hookspool.Done: %w", err)
	}
	return nil
}

// SetAside renames the live spool aside (stamped now) when it exists and is
// non-empty. It returns false when there was nothing to set aside.
func SetAside(path string, now time.Time) (bool, error) {
	if path == "" {
		return false, nil
	}
	st, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("hookspool.SetAside: %w", err)
	}
	if st.Size() == 0 {
		return false, nil
	}
	dst := path + asideMarker + strconv.FormatInt(now.UnixNano(), 10)
	if err := os.Rename(path, dst); err != nil {
		return false, fmt.Errorf("hookspool.SetAside: %w", err)
	}
	return true, nil
}
