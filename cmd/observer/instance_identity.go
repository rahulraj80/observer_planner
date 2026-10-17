package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
)

// instance_identity.go answers "which machine, on which build, is behind this
// forward?" for the instance switcher (T-425). D-325: a remote dashboard on a
// stale build (sb-devbox, schema 121) was opened through the switcher and its
// correct refusal was read as a fault on the operator's own node, because
// nothing said which machine or build it was.
//
// Once a forward is up, the daemon asks the REMOTE daemon's own /api/status
// through it (a loopback request to the forward's local port: no new network
// reach, the forward already exists) and keeps the version, schema version and
// hostname per forward. The switcher row shows them and flags an older build;
// the remote tab's own top bar does the same from its side.

// remoteStatus is the subset of a remote daemon's /api/status the switcher
// shows. An older remote build omits host_name; that stays empty.
type remoteStatus struct {
	Version       string `json:"version"`
	SchemaVersion int    `json:"schema_version"`
	HostName      string `json:"host_name"`
}

// remoteStatusTimeout bounds one probe. A forward that is up answers in
// milliseconds; a hung remote dashboard must not stall the switcher list.
const remoteStatusTimeout = 2 * time.Second

// probeRemoteStatus fetches <url>/api/status through a live forward. A
// package-level seam so tests never dial.
var probeRemoteStatus = func(baseURL string) (remoteStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), remoteStatusTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/status", nil)
	if err != nil {
		return remoteStatus{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return remoteStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return remoteStatus{}, fmt.Errorf("the remote dashboard answered %d", resp.StatusCode)
	}
	var st remoteStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&st); err != nil {
		return remoteStatus{}, fmt.Errorf("read the remote /api/status: %w", err)
	}
	return st, nil
}

// remoteProbeRetry is how long a FAILED probe is remembered before the next
// list asks again (a remote dashboard still starting up answers later).
const remoteProbeRetry = 30 * time.Second

// remoteIdentityCache remembers the probe result per profile, keyed to the
// forward's local port so a reconnect (a new port, maybe a different machine
// after an edit) is probed afresh. A success is kept for the life of the
// forward; a failure is retried after remoteProbeRetry. The zero value is
// ready to use.
type remoteIdentityCache struct {
	mu  sync.Mutex
	m   map[string]remoteIdentityEntry
	now func() time.Time
}

type remoteIdentityEntry struct {
	port int
	st   remoteStatus
	err  string
	at   time.Time
}

func (c *remoteIdentityCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// annotate fills the remote_* fields of a CONNECTED row, probing once per
// forward. Rows that are not connected are returned unchanged.
func (c *remoteIdentityCache) annotate(info dashapi.InstanceInfo) dashapi.InstanceInfo {
	if c == nil || info.State != "connected" || info.URL == "" || info.LocalPort == 0 {
		return info
	}
	c.mu.Lock()
	e, ok := c.m[info.Name]
	c.mu.Unlock()
	if !ok || e.port != info.LocalPort || (e.err != "" && c.clock().Sub(e.at) >= remoteProbeRetry) {
		st, err := probeRemoteStatus(info.URL)
		e = remoteIdentityEntry{port: info.LocalPort, st: st, at: c.clock()}
		if err != nil {
			e.err = remoteProbeError(info.Label, err)
		}
		c.mu.Lock()
		if c.m == nil {
			c.m = map[string]remoteIdentityEntry{}
		}
		c.m[info.Name] = e
		c.mu.Unlock()
	}
	info.RemoteVersion = e.st.Version
	info.RemoteSchemaVersion = e.st.SchemaVersion
	info.RemoteHostName = e.st.HostName
	info.RemoteProbeError = e.err
	return info
}

// remoteProbeError names the machine in the reason (T-425: errors from a
// remote node say which machine they came from).
func remoteProbeError(label string, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("%s: the remote dashboard did not answer within %s", label, remoteStatusTimeout)
	}
	return fmt.Sprintf("%s: could not read the remote dashboard's build: %v", label, err)
}
