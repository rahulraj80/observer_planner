package codeintelapi

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/archive"
)

// DisabledNote is the ONE operator-facing sentence every surface uses to
// say code intelligence is off by choice (the MCP symbol tools' note, the
// `observer index` / `observer codeintel` CLI, `observer doctor`). It names
// the dashboard control first, per the "no run-this-command dead end" rule.
const DisabledNote = "code intelligence is off (it is opt-in); turn it on in the dashboard under " +
	"Settings > Intelligence > Code intelligence, or set [codeintel].enabled = true in config.toml, " +
	"then restart the daemon"

// IsDisabled reports whether p is a provider built for an install with code
// intelligence turned off. It checks an optional interface rather than
// growing [Provider], so no other implementation (or test fake) has to
// change; anything that does not implement the hook reads as "not
// disabled".
func IsDisabled(p Provider) bool {
	d, ok := p.(interface{ CodeIntelDisabled() bool })
	return ok && d.CodeIntelDisabled()
}

// Unavailable returns a [Provider] that reports Available()==false and
// answers every query empty. A build without the codeintel component uses it
// in place of the native engine (lane MP-L1H, T-715).
func Unavailable() Provider { return off{} }

// Disabled is [Unavailable] that also reports IsDisabled(p) == true, so the
// MCP tools can say the module is off by choice.
func Disabled() Provider { return off{disabled: true} }

// off is the empty provider: every method fails open with an empty answer.
type off struct{ disabled bool }

func (o off) CodeIntelDisabled() bool { return o.disabled }

func (off) Available() bool   { return false }
func (off) Path() string      { return "codeintel:unavailable" }
func (off) Stale(string) bool { return false }
func (off) Close() error      { return nil }
func (off) SymbolsInFile(context.Context, string) ([]Symbol, error) {
	return nil, nil
}
func (off) FunctionsInFile(context.Context, string) ([]string, error) { return nil, nil }
func (off) ImportsInFile(context.Context, string) ([]string, error)   { return nil, nil }
func (off) FindSymbols(context.Context, string, string, string, string) ([]SymbolMatch, error) {
	return nil, nil
}
func (off) CallersOf(context.Context, string) ([]string, error)        { return nil, nil }
func (off) CallersOfSymbol(context.Context, int64, int) ([]Ref, error) { return nil, nil }
func (off) CalleesOfSymbol(context.Context, int64, int) ([]Ref, error) { return nil, nil }
func (off) CountCallers(context.Context, int64) (int, error)           { return 0, nil }
func (off) CountCallees(context.Context, int64) (int, error)           { return 0, nil }
func (off) CountEdgesByKind(context.Context, string) (int, error)      { return 0, nil }
func (off) Search(context.Context, string, string, int) ([]SymbolMatch, error) {
	return nil, nil
}

func (off) SemanticNeighbors(context.Context, int64, int) ([]SymbolMatch, error) {
	return nil, nil
}
func (off) SimilarTo(context.Context, int64, int) ([]SymbolMatch, error) { return nil, nil }
func (off) ListProjects(context.Context) ([]string, error)               { return nil, nil }
func (off) LoadGraph(context.Context, string) (Graph, error)             { return Graph{}, nil }
func (off) Reachable(context.Context, int64, RelationDirection, int, int) ([]Ref, bool, error) {
	return nil, false, nil
}

func (off) ArchivedProject(context.Context, string) (archive.Marker, bool, error) {
	return archive.Marker{}, false, nil
}
