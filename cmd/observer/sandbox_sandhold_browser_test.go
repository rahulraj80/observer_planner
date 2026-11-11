//go:build !sbo_omit_terminals_sandbox_sandhold && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import "testing"

// TestSandholdBrowserSafe: only a plain page address reaches the browser
// opener (on Windows it passes through cmd's start); anything else is
// printed for the person to open.
func TestSandholdBrowserSafe(t *testing.T) {
	cases := map[string]bool{
		"https://node.example.com/app/#/device?code=BCDF-GHJK": true,
		"http://127.0.0.1:8080/app/#/device?code=BCDF-GHJK":    true,
		"https://node.example.com/app/#/device?code=A&calc":    false,
		"https://node.example.com/%PATH%":                      false,
		"https://node.example.com/a\"b":                        false,
		"https://node.example.com/a|b":                         false,
		"https://node.example.com/a^b":                         false,
		"https://node.example.com/a b":                         false,
		"javascript:alert(1)":                                  false,
		"file:///etc/passwd":                                   false,
	}
	for page, want := range cases {
		if got := sandholdBrowserSafe.MatchString(page); got != want {
			t.Errorf("%q: %v, want %v", page, got, want)
		}
	}
}
