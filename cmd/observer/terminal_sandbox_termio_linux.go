//go:build linux && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import "golang.org/x/sys/unix"

// termEnterRawFlushed puts the terminal on fd in raw mode as term.MakeRaw
// does, but sets it with TCSETSF (tcsetattr TCSAFLUSH): input the terminal
// received and nothing read yet is discarded as the mode changes. Linux
// live check 2026-10-23 (R3, the OCI rows): a helper's terminal is still
// canonical while its box is made, and an end of file it receives in that
// time waits in the canonical buffer as the line discipline's NUL marker;
// set with TCSETS, that marker became a readable NUL byte the helper sent
// to the box as a keystroke, which the box's terminal echoed before the
// launcher's first line. A hosted box takes seconds to make, so the hosted
// helper enters raw mode the same way (live run 2026-11-11). What is
// discarded was typed under the host's own line editing before the box
// existed. It returns the restore.
func termEnterRawFlushed(fd int) (func(), error) {
	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, err
	}
	raw := *old
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETSF, &raw); err != nil {
		return nil, err
	}
	return func() { _ = unix.IoctlSetTermios(fd, unix.TCSETS, old) }, nil
}
