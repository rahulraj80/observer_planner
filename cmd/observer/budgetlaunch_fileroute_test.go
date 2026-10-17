package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFileRouteBudgetLaunchEvidence is T-208: a config-file route (cline-cli's
// openai-compatible provider in providers.json) is launch evidence for the
// managed budget check when the launcher's read-back of the selected entry's
// base URL is the proxy plus the route's suffix, and the operator passed no
// selector of their own.
func TestFileRouteBudgetLaunchEvidence(t *testing.T) {
	const proxy = "http://127.0.0.1:8820"
	at := func(base string, ok bool) func() (string, bool) {
		return func() (string, bool) { return base, ok }
	}
	cases := []struct {
		name     string
		tool     string
		readBack func() (string, bool)
		args     []string
		want     budgetLaunchRoute
	}{
		{"proxy base read back", "cline-cli", at(proxy+"/v1", true), nil, budgetLaunchRouteObserverProxy},
		{"trailing slash tolerated", "cline-cli", at(proxy+"/v1/", true), []string{"-m", "gpt-4o"}, budgetLaunchRouteObserverProxy},
		{"entry points elsewhere", "cline-cli", at("https://api.openai.com/v1", true), nil, budgetLaunchRouteUnknown},
		{"another proxy port", "cline-cli", at("http://127.0.0.1:9999/v1", true), nil, budgetLaunchRouteUnknown},
		{"file unreadable", "cline-cli", at("", false), nil, budgetLaunchRouteUnknown},
		{"no read-back", "cline-cli", nil, nil, budgetLaunchRouteUnknown},
		{"operator -P selector", "cline-cli", at(proxy+"/v1", true), []string{"-P", "anthropic"}, budgetLaunchRouteUnknown},
		{"operator --provider selector", "cline-cli", at(proxy+"/v1", true), []string{"--provider=openai"}, budgetLaunchRouteUnknown},
		{"selector after -- is a prompt", "cline-cli", at(proxy+"/v1", true), []string{"--", "-P"}, budgetLaunchRouteObserverProxy},
		{"env-routed tool never uses a file read-back", "opencode", at(proxy+"/v1", true), nil, budgetLaunchRouteUnknown},
		{"unproven route", "goose", at(proxy+"/v1", true), nil, budgetLaunchRouteUnknown},
	}
	for _, c := range cases {
		got := fileRouteBudgetLaunchEvidence(c.tool, proxy, c.readBack, c.args)
		if got.Route != c.want {
			t.Errorf("%s: route = %v, want %v", c.name, got.Route, c.want)
		}
		if got.Route == budgetLaunchRouteObserverProxy && got.ProxyURL != proxy {
			t.Errorf("%s: proxy url = %q", c.name, got.ProxyURL)
		}
	}
}

// TestReadClineCompatBaseURLAt reads only the openai-compatible entry's
// settings.baseUrl.
func TestReadClineCompatBaseURLAt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	if _, ok := readClineCompatBaseURLAt(path); ok {
		t.Fatal("missing file read as ok")
	}
	for _, c := range []struct {
		body string
		want string
		ok   bool
	}{
		{`{"providers":{"openai-compatible":{"settings":{"baseUrl":"http://127.0.0.1:8820/v1","apiKey":"x"}}}}`, "http://127.0.0.1:8820/v1", true},
		{`{"providers":{"openai-compatible":{"settings":{}}}}`, "", false},
		{`{"providers":{"anthropic":{"settings":{"baseUrl":"http://127.0.0.1:8820/v1"}}}}`, "", false},
		{`not json`, "", false},
	} {
		if err := os.WriteFile(path, []byte(c.body), 0o600); err != nil {
			t.Fatal(err)
		}
		got, ok := readClineCompatBaseURLAt(path)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got %q %v, want %q %v", c.body, got, ok, c.want, c.ok)
		}
	}
}
