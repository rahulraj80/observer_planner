package store

// nullIntPtr maps a nil *int to SQL NULL and a non-nil one to its value. It
// lives here, not in benchmark.go (which compiles out with the benchmark
// component), because the terminal-run stores use it too.
func nullIntPtr(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
