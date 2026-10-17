package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRouteAgyAPIKeyMode pins audit P2: the launcher sets
// GOOGLE_GEMINI_BASE_URL only in agy's API-key mode (settings.json
// modelProvider "gemini"), never over a caller's own value, and says which
// mode it saw.
func TestRouteAgyAPIKeyMode(t *testing.T) {
	cases := []struct {
		name     string
		settings string // "" = no settings.json
		env      []string
		wantSet  bool
		wantMsg  string
	}{
		{name: "no settings: sign-in", wantMsg: "Google sign-in mode"},
		{name: "sign-in settings", settings: `{"permissions":{}}`, wantMsg: "Google sign-in mode"},
		{name: "key env alone is not the switch", settings: `{}`, env: []string{"GEMINI_API_KEY=x"}, wantMsg: "Google sign-in mode"},
		{name: "api-key mode routes", settings: `{"modelProvider":"gemini"}`, wantSet: true, wantMsg: "API-key mode: routing via http://127.0.0.1:8820"},
		{name: "caller value kept", settings: `{"modelProvider":"gemini"}`, env: []string{"GOOGLE_GEMINI_BASE_URL=https://mine.test"}, wantMsg: "already set (https://mine.test)"},
		{name: "bad json: sign-in", settings: `{`, wantMsg: "Google sign-in mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.settings != "" {
				dir := filepath.Join(home, ".gemini", "antigravity-cli")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(tc.settings), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got := map[string]string{}
			setenv := func(k, v string) error { got[k] = v; return nil }
			var stderr bytes.Buffer
			set := routeAgyAPIKeyMode(tc.env, home, "http://127.0.0.1:8820/", setenv, &stderr)
			if set != tc.wantSet {
				t.Fatalf("set = %v, want %v", set, tc.wantSet)
			}
			if tc.wantSet && got["GOOGLE_GEMINI_BASE_URL"] != "http://127.0.0.1:8820" {
				t.Fatalf("GOOGLE_GEMINI_BASE_URL = %q", got["GOOGLE_GEMINI_BASE_URL"])
			}
			if !tc.wantSet && len(got) != 0 {
				t.Fatalf("env set unexpectedly: %v", got)
			}
			if !strings.Contains(stderr.String(), tc.wantMsg) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tc.wantMsg)
			}
		})
	}
}
