package sshforward

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/sshprofile"
)

// newSourcedManager builds a Manager reading its profiles from a live Source
// the test can swap, as the daemon's terminal config holder feeds it (T-488).
func newSourcedManager(t *testing.T, sp Spawner, cur *atomic.Pointer[Config]) *Manager {
	t.Helper()
	return newTestManager(t, sp, func(o *Options) {
		o.Enabled, o.Profiles = false, nil // ignored once a Source is wired
		o.Source = func() Config { return *cur.Load() }
	})
}

// TestSourceProfilesBindWithoutRestart pins T-488: a profile added through the
// dashboard editor is connectable on the next call, with no rebuild of the
// Manager (no daemon restart).
func TestSourceProfilesBindWithoutRestart(t *testing.T) {
	t.Parallel()
	var cur atomic.Pointer[Config]
	cur.Store(&Config{Enabled: true})
	sp := &fakeSpawner{}
	m := newSourcedManager(t, sp, &cur)

	if _, err := m.Connect("sb-devbox"); !errors.Is(err, ErrUnknownProfile) {
		t.Fatalf("Connect before the profile exists = %v, want ErrUnknownProfile", err)
	}
	cur.Store(&Config{Enabled: true, Profiles: testProfiles()})
	if got := m.List(); len(got) != 2 {
		t.Fatalf("List after the save = %d rows, want 2", len(got))
	}
	if inst, err := m.Connect("sb-devbox"); err != nil || inst.State != StateConnected {
		t.Fatalf("Connect after the save = %+v, %v, want connected", inst, err)
	}
}

// TestSourceRetiresForwardsOfRemovedOrEditedProfiles pins the safety half: a
// forward opened for a profile that is then removed or EDITED (its host
// changed) is stopped on the next consultation, so the switcher can never keep
// forwarding to a machine the config no longer names. Switching the surface
// off closes every forward.
func TestSourceRetiresForwardsOfRemovedOrEditedProfiles(t *testing.T) {
	t.Parallel()
	var cur atomic.Pointer[Config]
	cur.Store(&Config{Enabled: true, Profiles: testProfiles()})
	sp := &fakeSpawner{}
	m := newSourcedManager(t, sp, &cur)

	if _, err := m.Connect("sb-devbox"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Connect("other"); err != nil {
		t.Fatal(err)
	}

	// Edit sb-devbox's host; leave "other" untouched.
	edited := testProfiles()
	edited[0].Host = "a-different-machine.internal"
	cur.Store(&Config{Enabled: true, Profiles: edited})
	rows := m.List()
	if sp.proc(0).stopCount() != 1 {
		t.Fatal("the forward of an edited profile was not stopped")
	}
	if sp.proc(1).stopCount() != 0 {
		t.Fatal("an unchanged profile's forward was stopped")
	}
	for _, r := range rows {
		if r.Name == "sb-devbox" && r.State != StateDisconnected {
			t.Fatalf("edited profile row state = %q, want disconnected", r.State)
		}
		if r.Name == "other" && r.State != StateConnected {
			t.Fatalf("unchanged profile row state = %q, want connected", r.State)
		}
	}

	// Remove "other".
	cur.Store(&Config{Enabled: true, Profiles: edited[:1]})
	if _, ok := m.Get("other"); ok {
		t.Fatal("a removed profile is still listed")
	}
	if sp.proc(1).stopCount() != 1 {
		t.Fatal("the forward of a removed profile was not stopped")
	}

	// Switch the surface off: everything closes and Connect refuses.
	if _, err := m.Connect("sb-devbox"); err != nil {
		t.Fatal(err)
	}
	cur.Store(&Config{Enabled: false, Profiles: edited[:1]})
	if _, err := m.Connect("sb-devbox"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Connect with the surface off = %v, want ErrDisabled", err)
	}
	if sp.proc(2).stopCount() != 1 {
		t.Fatal("switching the surface off did not stop the live forward")
	}
}

// TestTestProfileProbesADraft pins the editor's test-connect: a draft that is
// not in the config is validated and probed, nothing is spawned through the
// forward path, and a draft that fails validation is refused before the probe.
func TestTestProfileProbesADraft(t *testing.T) {
	t.Parallel()
	var ran []string
	sp := &fakeSpawner{}
	m := newTestManager(t, sp, func(o *Options) {
		o.Enabled = false // a draft test does not need the surface on
		o.TestRunner = func(argv []string) (bool, string, error) {
			ran = argv
			return true, "", nil
		}
	})
	draft := sshprofile.Profile{Name: "new-box", Host: "new.internal", User: "dev"}
	res, err := m.TestProfile(draft)
	if err != nil || !res.AuthOK {
		t.Fatalf("TestProfile(draft) = %+v, %v, want auth ok", res, err)
	}
	found := false
	for _, a := range ran {
		if a == "new.internal" {
			found = true
		}
	}
	if !found {
		t.Fatalf("probe argv %v does not target the draft host", ran)
	}
	if sp.count() != 0 {
		t.Fatalf("a draft test spawned %d forwards, want 0", sp.count())
	}
	bad := sshprofile.Profile{Name: "Bad Name", Host: "-oProxyCommand=evil"}
	if _, err := m.TestProfile(bad); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("TestProfile(invalid) = %v, want ErrInvalidProfile", err)
	}
	if _, err := m.TestProfile(sshprofile.Profile{Name: "k", Host: "h.internal", KeyPath: "/definitely/not/a/key"}); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("TestProfile(missing key file) = %v, want ErrInvalidProfile", err)
	}
}
