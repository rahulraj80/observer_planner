//go:build !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"errors"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/sandboxbackend"
	"github.com/marmutapp/superbased-observer/internal/sandboxseccomp"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
	"github.com/marmutapp/superbased-observer/internal/termrun"
	"github.com/marmutapp/superbased-observer/internal/termsvc"
)

// TestSeccompRunVocabularyNamesTheProfile: the run state that says
// Observer's filter was applied is the profile's own id, so the one value
// names the filter (tracker F-002).
func TestSeccompRunVocabularyNamesTheProfile(t *testing.T) {
	if string(vocab.SeccompRunObserverDefault) != string(sandboxseccomp.ProfileObserverDefault) {
		t.Fatalf("SeccompRunObserverDefault %q, profile %q", vocab.SeccompRunObserverDefault, sandboxseccomp.ProfileObserverDefault)
	}
	if len(sandboxseccomp.Profiles()) != 1 {
		t.Fatal("a second seccomp profile needs its own SeccompRun value")
	}
}

// TestWithSeccompRun: the one stamping seam records the state from the
// stack's registry rows and the key, leaves a stackless run and a failed
// create alone (audit N2-A9: a key-on run on a row that loads no filter
// is named not_applicable, never silently unfiltered).
func TestWithSeccompRun(t *testing.T) {
	var noFilter string
	for _, id := range sandboxbackend.IDs() {
		if row, _ := sandboxbackend.Get(id); row.Iso.Seccomp == vocab.SeccompNone || row.Iso.Seccomp == "" {
			noFilter = id
			break
		}
	}
	if noFilter == "" {
		t.Fatal("no registry row without a filter to test with")
	}
	for _, c := range []struct {
		name  string
		keyOn bool
		stack []string
		err   error
		want  string
	}{
		{"bwrap, key on", true, []string{sandboxbackend.BackendBwrap}, nil, "observer_default"},
		{"bwrap, key off", false, []string{sandboxbackend.BackendBwrap}, nil, "off"},
		{"a row without a filter, key on", true, []string{noFilter}, nil, "not_applicable"},
		{"a row without a filter, key off", false, []string{noFilter}, nil, "none"},
		{"no stack", true, nil, nil, ""},
		{"an unknown id only", true, []string{"no-such-backend"}, nil, "not_applicable"},
		{"a failed create", true, []string{sandboxbackend.BackendBwrap}, errors.New("x"), ""},
	} {
		r := &sandboxRuntime{cfg: config.TerminalSandboxConfig{Seccomp: c.keyOn}}
		res, err := r.withSeccompRun(termsvc.CreateResult{Isolation: termrun.Isolation{Stack: c.stack}}, c.err)
		if !errors.Is(err, c.err) || res.Isolation.Seccomp != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, res.Isolation.Seccomp, err, c.want)
		}
	}
}

// TestSandboxCLISeccompLabel: the launch line says the key-on states and
// nothing else, so a key-off launch reads as before.
func TestSandboxCLISeccompLabel(t *testing.T) {
	for state, wantEmpty := range map[string]bool{
		"observer_default": false, "not_applicable": false,
		"off": true, "none": true, "engine_default": true, "": true,
	} {
		if got := sandboxCLISeccompLabel(state); (got == "") != wantEmpty {
			t.Errorf("%q: label %q", state, got)
		}
	}
}
