//go:build !sbo_omit_benchmark

package dashboard

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/arena"
	"github.com/marmutapp/superbased-observer/internal/arenamodels"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// arena_models.go - GET /api/arena/models, the read-only source of the Arena
// tab's judge and candidate model pickers. It loads the three inputs (the
// registry row, this node's model history, the compiled price-snapshot
// catalogue) and hands them to the pure internal/arenamodels.Compose; every
// grouping decision lives there, never here.

const (
	// arenaModelsWindow bounds the history query: a year keeps "models this
	// machine has used" honest without an unbounded token_usage scan.
	arenaModelsWindow = 365 * 24 * time.Hour
	// arenaModelsToolLimit / arenaModelsNodeLimit cap the per-harness and the
	// node-wide history lists.
	arenaModelsToolLimit = 20
	arenaModelsNodeLimit = 40
)

// arenaHarnessModels is one harness row of the response: the composed picker
// model plus whether its binary resolves on this host.
type arenaHarnessModels struct {
	arenamodels.Harness
	Installed bool `json:"installed"`
}

// arenaModelsResponse is the GET /api/arena/models body.
type arenaModelsResponse struct {
	// Routed reports that Arena drives go through the Observer proxy, which
	// is what makes a lane's provider prefix binding.
	Routed    bool                 `json:"routed"`
	Harnesses []arenaHarnessModels `json:"harnesses"`
}

// arenaProxyURL is the proxy base every Arena drive uses (shared with
// newArenaRunner so the picker's routed-lane rule and the runner agree).
func (s *Server) arenaProxyURL() string {
	return arenaProxyURLFor(s.opts.ProxyPort)
}

// handleArenaModels serves GET /api/arena/models[?harness=<id>]. Without a
// harness it lists every Arena harness (vendor catalogues first, then open
// ones, each alphabetical). History is fail-soft: a store error leaves the
// "used" group empty rather than failing the picker.
func (s *Server) handleArenaModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tools := arena.Harnesses()
	if want := strings.TrimSpace(r.URL.Query().Get("harness")); want != "" {
		found := false
		for _, t := range tools {
			if t == want {
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "unknown arena harness", http.StatusNotFound)
			return
		}
		tools = []string{want}
	}

	st := s.remoteManageStore()
	ctx := r.Context()
	node := arenaModelUsage(ctx, st, "", arenaModelsNodeLimit)
	catalog := cost.SnapshotModels()
	proxyURL := s.arenaProxyURL()

	out := arenaModelsResponse{Routed: proxyURL != "", Harnesses: make([]arenaHarnessModels, 0, len(tools))}
	for _, tool := range tools {
		capRow, _ := integration.For(tool)
		h := arenamodels.Compose(arenamodels.Input{
			Tool:       tool,
			Capability: capRow,
			ToolUsage:  arenaModelUsage(ctx, st, tool, arenaModelsToolLimit),
			NodeUsage:  node,
			Catalog:    catalog,
			Routed:     proxyURL != "",
		})
		out.Harnesses = append(out.Harnesses, arenaHarnessModels{Harness: h, Installed: arena.HarnessInstalled(tool)})
	}
	sort.SliceStable(out.Harnesses, func(i, j int) bool {
		vi := out.Harnesses[i].Catalog == string(integration.ModelCatalogVendor)
		vj := out.Harnesses[j].Catalog == string(integration.ModelCatalogVendor)
		if vi != vj {
			return vi
		}
		return out.Harnesses[i].Tool < out.Harnesses[j].Tool
	})
	writeJSON(w, out)
}

// arenaModelUsage loads model history for one tool ("" = every tool) and
// restates it in the pure package's type. Fail-soft: nil on any error.
func arenaModelUsage(ctx context.Context, st *store.Store, tool string, limit int) []arenamodels.Usage {
	if st == nil {
		return nil
	}
	rows, err := st.LoadModelUsage(ctx, tool, arenaModelsWindow, limit)
	if err != nil {
		return nil
	}
	out := make([]arenamodels.Usage, 0, len(rows))
	for _, u := range rows {
		out = append(out, arenamodels.Usage{Model: u.Model, Count: u.Count, LastUsed: u.LastUsed, Tools: u.Tools})
	}
	return out
}
