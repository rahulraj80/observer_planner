package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/componentstate"
)

// TestCompiledOutVerbRefuses: the verb a *_omit.go file registers refuses
// with exit code 3 and the "not in this build" line even when no component
// gate wraps it.
func TestCompiledOutVerbRefuses(t *testing.T) {
	cmd := compiledOutVerb("process", "x", "processobs")
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"status", "--json"})
	err := cmd.Execute()
	var ee exitErr
	if !errors.As(err, &ee) || int(ee) != componentstate.RefusalExitCode {
		t.Fatalf("err = %v, want exit code %d", err, componentstate.RefusalExitCode)
	}
	if !strings.Contains(errBuf.String(), "Process observation (processobs) is not in this build") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}
