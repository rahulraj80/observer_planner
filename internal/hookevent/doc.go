// Package hookevent is the normalized, capability-shaped form of one hook
// invocation (hook-parity audit 2026-10-02, section 5.3;
// docs/plans/hook-parity-lane0-brief.md step 8).
//
// Every vendor's hook payload is mapped at the receiver boundary into one
// Event (which capability fired, the session, the working directory, the
// canonical action type and operand from internal/tooltax, the prompt, the
// process relation) and every capability handler answers with one Outcome
// (decision, reasons, injected context, modified input, exit code) that the
// binding's reply dialect renders back into the vendor's wire shape.
//
// Pure data: no SQL, no HTTP, no fsnotify (imports_test.go pins it). The
// receiver in cmd/observer owns the I/O; this package only names the shapes
// that cross the seam, so a feature written against a capability never sees
// a vendor's payload.
package hookevent
