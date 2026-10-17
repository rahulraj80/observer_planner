package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// orgclient_cfgedit.go owns the [org_client] keys that leaving and
// re-joining an organization rewrite in the node's own config.toml (T-728).
//
// Before T-728 a leave kept the whole block, so config.toml went on saying
// `enabled = true` with the address of an organization the node had left (the
// operator's node pointed at a retired server URL after its leave, D-542), and
// a re-enrol never rewrote an existing block, so the stale address stayed
// through the next enrolment as well. Now both edges edit exactly the rows
// below, as comment-preserving one-line edits (config.PatchScalarsOnly, the
// dashboard config editor's surgical path without its whole-file fallback);
// every other line of the file, the share and scope blocks included, is kept.
// A block that is absent is never created here (the enrolment's
// ensureOrgClientBlock writes a fresh one), and an edit that cannot be made
// surgically writes nothing and says why.

// orgClientCfgEdge names when a row applies.
type orgClientCfgEdge int

const (
	// orgClientCfgLeave is a completed leave (`observer unenroll`, Leave).
	orgClientCfgLeave orgClientCfgEdge = iota
	// orgClientCfgEnrol is an enrolment into a block that already exists.
	orgClientCfgEnrol
)

// orgClientCfgRow is one [org_client] key and what each edge writes to it.
// value returns the rendered TOML right-hand side, or ok=false to leave the
// key as it is on that edge.
type orgClientCfgRow struct {
	key   string
	value func(edge orgClientCfgEdge, serverURL string) (rhs string, ok bool)
}

// orgClientCfgRows is the whole table, one row per key, walked in order.
var orgClientCfgRows = []orgClientCfgRow{
	{
		// The push switch: off once the node has left (a daemon started later
		// runs no push loop and no policy-state poster for an organization it
		// no longer belongs to), on again when it joins one.
		key: "org_client.enabled",
		value: func(edge orgClientCfgEdge, _ string) (string, bool) {
			if edge == orgClientCfgLeave {
				return "false", true
			}
			return "true", true
		},
	},
	{
		// The server address: kept on a leave (it names where the node was,
		// and the enrolment row that the client actually reads is gone), and
		// rewritten to the server just joined on an enrolment.
		key: "org_client.org_server_url",
		value: func(edge orgClientCfgEdge, serverURL string) (string, bool) {
			u := strings.TrimSpace(serverURL)
			if edge != orgClientCfgEnrol || u == "" {
				return "", false
			}
			return strconv.Quote(u), true
		},
	},
}

// orgClientCfgEdit is what applyOrgClientConfigEdge did.
type orgClientCfgEdit struct {
	// Path is the config file considered.
	Path string
	// Applied names each key whose line now reads its new value
	// ("org_client.enabled = false").
	Applied []string
	// Wrote is true when the file changed.
	Wrote bool
	// NoBlock is true when the file has no [org_client] table, so nothing
	// was edited.
	NoBlock bool
}

// applyOrgClientConfigEdge applies the edge's rows to the [org_client] table
// of the config file at path. A missing file or a missing table edits
// nothing. The error is set, and nothing written, when the edit cannot be a
// comment-preserving line edit.
func applyOrgClientConfigEdge(path string, edge orgClientCfgEdge, serverURL string) (orgClientCfgEdit, error) {
	out := orgClientCfgEdit{Path: path}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		out.NoBlock = true
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("read %s: %w", path, err)
	}
	if !hasOrgClientTableHeader(string(body)) {
		out.NoBlock = true
		return out, nil
	}
	var patches []config.Patch
	for _, row := range orgClientCfgRows {
		rhs, ok := row.value(edge, serverURL)
		if !ok {
			continue
		}
		patches = append(patches, config.Patch{Dotted: row.key, RHS: rhs, Scalar: true})
		out.Applied = append(out.Applied, row.key+" = "+rhs)
	}
	if len(patches) == 0 {
		return out, nil
	}
	res, err := config.PatchScalarsOnly(path, patches)
	if err != nil {
		out.Applied = nil
		return out, err
	}
	out.Wrote = res.Wrote
	return out, nil
}
