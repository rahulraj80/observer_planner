package llmconn

// Select returns the enabled connection best suited for task. When hint names
// a usable connection, it wins. Otherwise a local (no-egress) connection is
// preferred for privacy; the first enabled usable connection is the fallback.
// The second return is false when no connection can serve the task.
func Select(conns []Connection, task Task, hint string) (Connection, bool) {
	if hint != "" {
		for _, c := range conns {
			if c.ID == hint && usable(c, task) {
				return c, true
			}
		}
	}
	var first *Connection
	for i := range conns {
		c := &conns[i]
		if !usable(*c, task) {
			continue
		}
		if first == nil {
			first = c
		}
		if c.Local() {
			return *c, true
		}
	}
	if first != nil {
		return *first, true
	}
	return Connection{}, false
}
