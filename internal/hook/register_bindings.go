package hook

import "github.com/marmutapp/superbased-observer/internal/integration"

// registeredEvents returns the native events the registry row for tool
// binds as Registered, in binding order, de-duplicated: the event list a
// per-format writer registers (hook-parity audit section 5.4: writers take
// their event list from the bindings, not from a per-vendor slice). nil for
// an unknown tool or a row that registers nothing.
func registeredEvents(tool string) []string {
	c, ok := integration.For(tool)
	if !ok {
		return nil
	}
	return c.Hook.RegisteredEvents()
}

// eventsOr returns registeredEvents(tool), or fallback when the registry
// declares none (a writer is never left with an empty list by a registry
// row that has not grown bindings).
func eventsOr(tool string, fallback ...string) []string {
	if ev := registeredEvents(tool); len(ev) > 0 {
		return ev
	}
	return fallback
}
