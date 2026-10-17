package orgclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
)

func TestProbeServer(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    error
	}{
		{
			name: "org server",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" || r.Header.Get("Authorization") != "" {
					http.Error(w, "unexpected", http.StatusTeapot)
					return
				}
				_, _ = w.Write([]byte(`{"status":"ok","roles":["all"]}`))
			},
		},
		{
			name:    "no healthz",
			handler: func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) },
			want:    ErrNotOrgServer,
		},
		{
			name:    "a web page, not the health document",
			handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>hi</html>")) },
			want:    ErrNotOrgServer,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			c := New(config.OrgClientConfig{}, nil, nil, "test", srv.Client(), nil)
			err := c.ProbeServer(context.Background(), srv.URL+"/")
			if tc.want == nil && err != nil {
				t.Fatalf("ProbeServer = %v, want nil", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("ProbeServer = %v, want %v", err, tc.want)
			}
		})
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	dead := srv.URL
	srv.Close()
	c := New(config.OrgClientConfig{}, nil, nil, "test", nil, nil)
	if err := c.ProbeServer(context.Background(), dead); !errors.Is(err, ErrServerUnreachable) {
		t.Fatalf("dead server = %v, want ErrServerUnreachable", err)
	}
}
