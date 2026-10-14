package routeprov

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

const (
	path  = "/home/dev/.claude/settings.json"
	value = "http://127.0.0.1:8820"
)

func rec(w Writer) Record {
	return Record{Tool: "claude-code", ConfigPath: path, Value: value, Writer: w, WrittenAt: "t"}
}

// TestNoteClaimRule: one case per rule in Note's doc comment.
func TestNoteClaimRule(t *testing.T) {
	cases := []struct {
		name       string
		start      []Record
		write      Record
		outcome    Outcome
		wantChange bool
		wantWriter Writer // "" = no record
	}{
		{"nothing happened", nil, rec(WriterEnrolment), OutcomeNone, false, ""},
		{"enrolment wrote it", nil, rec(WriterEnrolment), OutcomeWrote, true, WriterEnrolment},
		{"init wrote it", nil, rec(WriterInit), OutcomeWrote, true, WriterInit},
		{"enrolment found it already set, no record", nil, rec(WriterEnrolment), OutcomeConfirmed, false, ""},
		{"enrolment found its own route again", []Record{rec(WriterEnrolment)}, rec(WriterEnrolment), OutcomeConfirmed, false, WriterEnrolment},
		{"enrolment found init's route", []Record{rec(WriterInit)}, rec(WriterEnrolment), OutcomeConfirmed, false, WriterInit},
		{"init confirmed enrolment's route", []Record{rec(WriterEnrolment)}, rec(WriterInit), OutcomeConfirmed, true, WriterInit},
		{"setup confirmed a route with no record", nil, rec(WriterSetup), OutcomeConfirmed, true, WriterSetup},
		{"enrolment overwrote init's route", []Record{rec(WriterInit)}, rec(WriterEnrolment), OutcomeWrote, true, WriterEnrolment},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := File{Version: FileVersion, Records: append([]Record(nil), c.start...)}
			if got := f.Note(c.write, c.outcome); got != c.wantChange {
				t.Fatalf("Note changed = %v, want %v", got, c.wantChange)
			}
			r := f.Find("claude-code", path)
			switch {
			case c.wantWriter == "" && r != nil:
				t.Fatalf("record = %+v, want none", r)
			case c.wantWriter != "" && (r == nil || r.Writer != c.wantWriter):
				t.Fatalf("record = %+v, want writer %q", r, c.wantWriter)
			}
			if len(f.Records) > 1 {
				t.Fatalf("one target must have one record, got %d", len(f.Records))
			}
		})
	}
}

// TestNoteConfirmationKeepsPriorProvider: a confirmation cannot know what
// the original write replaced, so the recorded prior provider survives.
func TestNoteConfirmationKeepsPriorProvider(t *testing.T) {
	first := Record{Tool: "codex", ConfigPath: "/c", Value: "v", Writer: WriterEnrolment, PriorProvider: "azure"}
	f := File{Records: []Record{first}}
	f.Note(Record{Tool: "codex", ConfigPath: "/c", Value: "v", Writer: WriterInit}, OutcomeConfirmed)
	if r := f.Find("codex", "/c"); r == nil || r.PriorProvider != "azure" || r.Writer != WriterInit {
		t.Fatalf("record = %+v", r)
	}
	f.Drop("codex", "/c")
	if f.Find("codex", "/c") != nil {
		t.Fatal("Drop left the record")
	}
}

// TestDecideTable: one case per row of decideRules, plus the messages the
// Leave dialog shows.
func TestDecideTable(t *testing.T) {
	enrol := rec(WriterEnrolment)
	initRec := rec(WriterInit)
	cases := []struct {
		name    string
		in      DecideInput
		action  Action
		reason  Reason
		message string
	}{
		{"not set", DecideInput{Tool: "claude-code", ConfigPath: path, Record: &enrol, Removable: true}, ActionNone, ReasonNotSet, ""},
		{
			"set with no record",
			DecideInput{Tool: "claude-code", ConfigPath: path, Present: true, Current: value, Removable: true},
			ActionKeep, ReasonNoRecord,
			"Your Claude Code proxy setting will be kept because you set it yourself.",
		},
		{
			"set by init",
			DecideInput{Tool: "claude-code", ConfigPath: path, Present: true, Current: value, Record: &initRec, Removable: true},
			ActionKeep, ReasonOtherWriter,
			"Your Claude Code proxy setting will be kept because `observer init` set it.",
		},
		{
			"changed after enrolment",
			DecideInput{Tool: "claude-code", ConfigPath: path, Present: true, Current: "http://127.0.0.1:9999", Record: &enrol, Removable: true},
			ActionKeep, ReasonChangedAfterEnrol,
			"Your Claude Code proxy setting will be kept because it changed after enrolment wrote it.",
		},
		{
			"enrolment wrote it but the tool has no remover",
			DecideInput{Tool: "kimi-code", ConfigPath: "/k", Present: true, Current: "v", Record: &Record{Tool: "kimi-code", ConfigPath: "/k", Value: "v", Writer: WriterEnrolment}},
			ActionKeep, ReasonCannotRemoveForTool,
			"Enrolment wrote the Kimi Code proxy route in /k, but Leave cannot undo this tool's route; remove it there by hand.",
		},
		{
			"written by enrolment",
			DecideInput{Tool: "claude-code", ConfigPath: path, Present: true, Current: value, Record: &enrol, Removable: true, Setting: "ANTHROPIC_BASE_URL"},
			ActionRemove, ReasonWrittenByEnrolment,
			"Removes ANTHROPIC_BASE_URL from " + path + " (written when this machine enrolled).",
		},
		{
			// T-495: the setting comes from the boundary, never from the
			// tool name, so a route target this package has never heard of
			// reads its own setting, and one with none reads generically.
			"written by enrolment, setting resolved at the boundary",
			DecideInput{Tool: "some-new-tool", ConfigPath: "/n", Present: true, Current: "v", Record: &Record{Tool: "some-new-tool", ConfigPath: "/n", Value: "v", Writer: WriterEnrolment}, Removable: true, Setting: "BASE_URL_X"},
			ActionRemove, ReasonWrittenByEnrolment,
			"Removes BASE_URL_X from /n (written when this machine enrolled).",
		},
		{
			"written by enrolment, no setting named",
			DecideInput{Tool: "claude-code", ConfigPath: path, Present: true, Current: value, Record: &enrol, Removable: true},
			ActionRemove, ReasonWrittenByEnrolment,
			"Removes the Observer proxy route from " + path + " (written when this machine enrolled).",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := Decide(c.in)
			if d.Action != c.action || d.Reason != c.reason || d.Message != c.message {
				t.Fatalf("Decide = %+v\nwant action %s reason %s message %q", d, c.action, c.reason, c.message)
			}
			if strings.Contains(d.Message, "—") {
				t.Fatalf("message has an em dash: %q", d.Message)
			}
		})
	}
}

// TestPackageIsPure pins the package free of I/O imports (CLAUDE.md #1).
func TestPackageIsPure(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{"database/sql", "net/http", "os", "os/exec", "github.com/fsnotify/fsnotify"},
		Reason:    "internal/routeprov must stay pure",
	})
}

// TestNoCaseOnAStringLiteral pins T-495 (CLAUDE.md rule 3): no switch in
// this package selects on a string literal, the shape a tool-name branch
// takes ("case \"claude-code\":"). Decide and message dispatch on the typed
// Reason / Writer constants; per-tool facts arrive in DecideInput.
func TestNoCaseOnAStringLiteral(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		fs := token.NewFileSet()
		af, err := parser.ParseFile(fs, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				if bl, ok := e.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					t.Errorf("%s: case on string literal %s; resolve it at the boundary instead", fs.Position(bl.Pos()), bl.Value)
				}
			}
			return true
		})
	}
}
