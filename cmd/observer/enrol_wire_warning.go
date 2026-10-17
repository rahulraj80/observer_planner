package main

import (
	"fmt"
	"io"
	"strings"
)

// printProxyDownWarning is the post-enrolment warning for a stopped proxy.
// It names only the tools whose route is in place (T-736, D-548): a node
// with no Claude Code used to be told that Claude Code "now routes through
// the proxy" although nothing had been written. An empty list prints
// nothing.
func printProxyDownWarning(out io.Writer, port int, routed []string) {
	if len(routed) == 0 {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "WARNING: the observer proxy on 127.0.0.1:%d is not running.\n", port)
	for _, t := range routed {
		if strings.TrimSuffix(t, "-windows") == "claude-code" {
			fmt.Fprintln(out, "  Claude Code now routes through the proxy via ANTHROPIC_BASE_URL in")
			fmt.Fprintln(out, "  ~/.claude/settings.json, so every Claude Code session on this host")
			fmt.Fprintln(out, "  will fail with connection-refused until the proxy starts.")
			break
		}
	}
	fmt.Fprintf(out, "  Routed through it now: %s.\n", strings.Join(routed, ", "))
	fmt.Fprintln(out, "  Run `observer start` in another shell (or `observer proxy start`).")
}
