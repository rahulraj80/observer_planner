package orgenrol

import "testing"

func TestDisplayNameNeverAnID(t *testing.T) {
	const id = "a57ee8e19d6e97f943e88322a8482903"
	cases := []struct {
		name, url string
		names     []string
		want      string
	}{
		{"real name wins", "https://org.acme.test", []string{"Acme"}, "Acme"},
		{"first real name in order", "https://org.acme.test", []string{"", "Acme", "Other"}, "Acme"},
		{"id as name skipped, host used", "https://org.acme.test:8443/x", []string{id}, "org.acme.test:8443"},
		{"other id-shaped name skipped", "https://org.acme.test", []string{"0123456789ABCDEF0123456789abcdef"}, "org.acme.test"},
		{"blank names, host used", "http://127.0.0.1:18640", []string{"  "}, "127.0.0.1:18640"},
		{"nothing usable", "", []string{id, ""}, ""},
		{"unparseable url", "::not a url", nil, ""},
		// The governance surfaces' rows (folded in from cmd/observer's former
		// governanceDisplayName): the enrolment name first, the grant row's
		// org_name (which carries the id) second.
		{"enrolment name wins over the grant's id", "https://org.acme.test", []string{"Acme Corp", id}, "Acme Corp"},
		{"id in upper case is still an id", "", []string{"A57EE8E19D6E97F943E88322A8482903"}, ""},
		{"names are trimmed", "", []string{"  Acme  "}, "Acme"},
		{"nothing at all", "", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DisplayName(id, c.url, c.names...); got != c.want {
				t.Fatalf("DisplayName = %q, want %q", got, c.want)
			}
		})
	}
}
