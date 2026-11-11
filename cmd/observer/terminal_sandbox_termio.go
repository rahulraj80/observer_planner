//go:build !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"bytes"
	"io"
)

// termNoNUL drops NUL bytes from a box's terminal output on its way to the
// helper's terminal (first the OCI rows, live check 2026-10-22; then the
// hosted rows, live run 2026-11-11 on a SandHold node, where a NUL came
// before the in-box Observer's first line). A NUL draws nothing on a
// terminal, but a typescript, a log or a dashboard capture shows it. It is
// dropped only on the output side: Ctrl-@ is a real keystroke the agent
// may want.
type termNoNUL struct{ w io.Writer }

// Write writes p without its NUL bytes and reports all of p as written.
func (n termNoNUL) Write(p []byte) (int, error) {
	if bytes.IndexByte(p, 0) < 0 {
		return n.w.Write(p)
	}
	if q := bytes.ReplaceAll(p, []byte{0}, nil); len(q) > 0 {
		if _, err := n.w.Write(q); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}
