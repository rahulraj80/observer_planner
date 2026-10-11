package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
)

// TestWriteCompiledOutShape pins the body every *_omit.go handler stub
// answers: the route guard's closed component_not_installed shape with
// state absent and reason not_compiled, naming the component and its title.
func TestWriteCompiledOutShape(t *testing.T) {
	rec := httptest.NewRecorder()
	writeCompiledOut(rec, "processobs")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body componentstate.NotActiveBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != componentstate.NotActiveError || body.Component != "processobs" ||
		body.State != components.StateAbsent || body.Reason != components.ReasonNotCompiled {
		t.Fatalf("body = %+v", body)
	}
	if !strings.Contains(body.Message, "Process observation (processobs) is not in this build") {
		t.Errorf("message = %q", body.Message)
	}
}
