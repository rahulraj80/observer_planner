package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/costtext"
)

// maskDollarProse hides dollar amounts written into PROSE fields of a JSON
// response when the developer cost display is the token view (D-013). The web
// client already renders numeric dollar fields under the display mode; what it
// cannot re-denominate is server-written text ("you spent $4.10", a routing
// benefit line, an LLM answer that echoes a figure). Used on the Briefing and
// routing read routes, whose composers format dollars into prose
// (internal/briefing, internal/llmrun, internal/routing/benefit.go). Fields and
// numbers are kept (costtext.MaskStrings); non-JSON responses pass unchanged.
func (s *Server) maskDollarProse(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !costtext.Tokens(s.statuslineCostDisplay(r.Context())) {
			next(w, r)
			return
		}
		rec := &bufferedResponse{header: http.Header{}, status: http.StatusOK}
		next(rec, r)
		body := rec.body.Bytes()
		if strings.HasPrefix(rec.header.Get("Content-Type"), "application/json") {
			var v any
			dec := json.NewDecoder(bytes.NewReader(body))
			dec.UseNumber()
			if err := dec.Decode(&v); err == nil {
				if masked, err := json.Marshal(costtext.MaskStrings(v)); err == nil {
					masked = append(masked, '\n')
					body = masked
					rec.header.Del("Content-Length")
				}
			}
		}
		for k, vals := range rec.header {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(rec.status)
		_, _ = w.Write(body)
	}
}

// bufferedResponse captures a handler's response so it can be reshaped.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header         { return b.header }
func (b *bufferedResponse) WriteHeader(code int)        { b.status = code }
func (b *bufferedResponse) Write(p []byte) (int, error) { return b.body.Write(p) }
