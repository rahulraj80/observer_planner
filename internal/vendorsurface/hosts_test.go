package vendorsurface

import (
	"strings"
	"testing"
)

func builtRow() Surface {
	s := validRow()
	s.Build = BuildBuilt
	s.SecretClass = SecretNever
	s.Supplies[0].Build = BuildBuilt
	s.Controls = nil
	s.Hosts = []HostTuple{{Scheme: "https", Host: "api.example.com", Port: 443}}
	s.Derivation.AuthRule = "plane: example admin API bearer"
	return s
}

// TestBuiltRowTable: one case per row of the Built-row table and of the host
// tuple check (lane F7: the runner reaches only catalog hosts, never an
// unclassified secret class or a keyword-derived auth kind, D-402).
func TestBuiltRowTable(t *testing.T) {
	if errs := Validate(builtRow()); len(errs) != 0 {
		t.Fatalf("base built row invalid: %v", errs)
	}
	cases := []struct {
		name string
		mut  func(*Surface)
		want string
	}{
		{"built pull surface without hosts", func(s *Surface) { s.Hosts = nil }, "at least one host tuple"},
		{"built with unclassified secret class", func(s *Surface) { s.SecretClass = SecretUnclassified }, "reviewed secret class"},
		{"built with keyword-derived auth", func(s *Surface) { s.Derivation.AuthRule = "keyword: bearer" }, "not keyword-derived"},
		{"built with unclassified auth", func(s *Surface) { s.Auth.Kind = AuthUnclassified }, "auth kind is classified"},
		{"plain http host", func(s *Surface) { s.Hosts[0].Scheme = "http" }, "scheme must be https"},
		{"ip literal host", func(s *Surface) { s.Hosts[0].Host = "203.0.113.9" }, "IP literal"},
		{"ipv6 literal host", func(s *Surface) { s.Hosts[0].Host = "[::1]" }, "IP literal"},
		{"single label host", func(s *Surface) { s.Hosts[0].Host = "localhost" }, "lower-case DNS name"},
		{"upper case host", func(s *Surface) { s.Hosts[0].Host = "API.example.com" }, "lower-case DNS name"},
		{"userinfo host", func(s *Surface) { s.Hosts[0].Host = "user@api.example.com" }, "lower-case DNS name"},
		{"port out of range", func(s *Surface) { s.Hosts[0].Port = 0 }, "port out of range"},
		{"duplicate host", func(s *Surface) { s.Hosts = append(s.Hosts, s.Hosts[0]) }, "listed twice"},
		// lane ENTAPI-D3 (T-692): the OAuth token step's host.
		{"built client-credentials row without a token host", func(s *Surface) {
			s.Auth.Kind = AuthOAuthClientCredentials
		}, "declares its token host"},
		{"token host on another auth kind", func(s *Surface) {
			s.TokenHosts = []HostTuple{{Scheme: "https", Host: "login.example.com", Port: 443}}
		}, "only by an oauth_client_credentials or cloud_iam surface"},
		{"plain http token host", func(s *Surface) {
			s.Auth.Kind = AuthOAuthClientCredentials
			s.TokenHosts = []HostTuple{{Scheme: "http", Host: "login.example.com", Port: 443}}
		}, "scheme must be https"},
		{"token host equal to the API host", func(s *Surface) {
			s.Auth.Kind = AuthOAuthClientCredentials
			s.TokenHosts = []HostTuple{s.Hosts[0]}
		}, "token host tuple"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := builtRow()
			s.Hosts = append([]HostTuple(nil), s.Hosts...)
			c.mut(&s)
			errs := Validate(s)
			for _, e := range errs {
				if strings.Contains(e.Error(), c.want) {
					return
				}
			}
			t.Fatalf("want an error containing %q, got %v", c.want, errs)
		})
	}
	// A buildable (not yet built) row is not held to the Built table.
	s := validRow()
	if errs := Validate(s); len(errs) != 0 {
		t.Fatalf("buildable row without hosts must stay valid: %v", errs)
	}
	// lane ENTAPI-L16C (T-710): a cloud_iam row may declare the Google token
	// host.
	g := builtRow()
	g.Hosts = append([]HostTuple(nil), g.Hosts...)
	g.Auth.Kind = AuthCloudIAM
	g.TokenHosts = []HostTuple{{Scheme: "https", Host: "oauth2.googleapis.com", Port: 443}}
	for _, e := range Validate(g) {
		if strings.Contains(e.Error(), "token host") {
			t.Fatalf("cloud_iam token host refused: %v", e)
		}
	}
}

// TestHostTemplateTable (lane L8, GHE.com data residency): one case per
// template rule, plus Expand and Match. A template has exactly one whole
// tenant label followed by at least two fixed labels; a download template
// needs an API template to bind its tenant.
func TestHostTemplateTable(t *testing.T) {
	ghe := HostTemplate{Scheme: "https", Host: "api.{tenant}.ghe.com", Port: 443}
	dl := HostTemplate{Scheme: "https", Host: "copilot-reports.{tenant}.ghe.com", Port: 443}
	base := func() Surface {
		s := builtRow()
		s.HostTemplates = []HostTemplate{ghe}
		s.DownloadHosts = []HostTuple{{Scheme: "https", Host: "copilot-reports.example.com", Port: 443}}
		s.DownloadHostTemplates = []HostTemplate{dl}
		return s
	}
	if errs := Validate(base()); len(errs) != 0 {
		t.Fatalf("base row invalid: %v", errs)
	}
	cases := []struct {
		name string
		mut  func(*Surface)
		want string
	}{
		{"no tenant label", func(s *Surface) { s.HostTemplates[0].Host = "api.ghe.com" }, "no {tenant} label"},
		{"two tenant labels", func(s *Surface) { s.HostTemplates[0].Host = "{tenant}.{tenant}.ghe.com" }, "more than one tenant label"},
		{"tenant too close to the end", func(s *Surface) { s.HostTemplates[0].Host = "api.ghe.{tenant}" }, "two fixed labels"},
		{"partial label", func(s *Surface) { s.HostTemplates[0].Host = "api.x{tenant}.ghe.com" }, "no {tenant} label"},
		{"plain http template", func(s *Surface) { s.HostTemplates[0].Scheme = "http" }, "scheme must be https"},
		{"template port", func(s *Surface) { s.HostTemplates[0].Port = 70000 }, "port out of range"},
		{"upper case suffix", func(s *Surface) { s.HostTemplates[0].Host = "api.{tenant}.GHE.com" }, "lower-case DNS name"},
		{"download template without an API template", func(s *Surface) { s.HostTemplates = nil }, "needs an API host template"},
		{"download host is an IP", func(s *Surface) { s.DownloadHosts[0].Host = "203.0.113.9" }, "IP literal"},
		{"download host repeats an API host", func(s *Surface) { s.DownloadHosts[0] = s.Hosts[0] }, "listed twice"},
		{"templates without an exact host", func(s *Surface) { s.Hosts = nil }, "at least one exact API host"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := base()
			s.HostTemplates = append([]HostTemplate(nil), s.HostTemplates...)
			s.DownloadHosts = append([]HostTuple(nil), s.DownloadHosts...)
			c.mut(&s)
			for _, e := range Validate(s) {
				if strings.Contains(e.Error(), c.want) {
					return
				}
			}
			t.Fatalf("want an error containing %q, got %v", c.want, Validate(s))
		})
	}
	h, ok := ghe.Expand("octocorp")
	if !ok || h != (HostTuple{"https", "api.octocorp.ghe.com", 443}) {
		t.Fatalf("Expand = %+v %v", h, ok)
	}
	for _, bad := range []string{"", "a.b", "Octo", "-x", "x_y", "evil.com/x"} {
		if _, ok := ghe.Expand(bad); ok {
			t.Errorf("Expand(%q) accepted", bad)
		}
	}
	if tn, ok := ghe.Match(h); !ok || tn != "octocorp" {
		t.Fatalf("Match = %q %v", tn, ok)
	}
	for _, other := range []HostTuple{
		{"https", "api.a.b.ghe.com", 443},
		{"https", "api..ghe.com", 443},
		{"https", "api.octocorp.ghe.com.evil.com", 443},
		{"https", "evilapi.octocorp.ghe.com", 443},
		{"http", "api.octocorp.ghe.com", 443},
		{"https", "api.octocorp.ghe.com", 8443},
		{"https", "api.ghe.com", 443},
	} {
		if tn, ok := ghe.Match(other); ok {
			t.Errorf("Match(%s) = %q", other, tn)
		}
	}
}

func TestParseHostTuple(t *testing.T) {
	cases := []struct {
		in   string
		want HostTuple
		bad  bool
	}{
		{in: "https://api.github.com", want: HostTuple{"https", "api.github.com", 443}},
		{in: "https://api.github.com:8443", want: HostTuple{"https", "api.github.com", 8443}},
		{in: "https://api.github.com/path", bad: true},
		{in: "https://user@api.github.com", bad: true},
		{in: "api.github.com", bad: true},
		{in: "https://api.github.com:x", bad: true},
	}
	for _, c := range cases {
		got, err := ParseHostTuple(c.in)
		if c.bad != (err != nil) || (!c.bad && got != c.want) {
			t.Errorf("ParseHostTuple(%q) = %+v, %v", c.in, got, err)
		}
	}
	if s := (HostTuple{"https", "api.github.com", 443}).String(); s != "https://api.github.com:443" {
		t.Errorf("String() = %q", s)
	}
}
