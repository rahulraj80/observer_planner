// Package pushsched is the org push loop's timing table (2026-10-02
// budget-latency arc, P3; docs/budgets.md "Push cadence").
//
// Before P3 a node pushed once per push interval and slept the whole interval
// even after a byte-capped batch, so a proxied turn reached the org dashboard
// up to one interval later (900 s on the estates whose enrolment wrote that
// value). Now node activity - a committed proxied turn or transcript row -
// pulls the next push forward, inside three bounds:
//
//   - a FLOOR between two pushes (default 15 s), raised by the org's
//     X-SBO-Push-Min-Interval backpressure hint;
//   - a COALESCE window: push Quiet (3 s) after the last activity, but never
//     later than MaxWait (10 s) after the first pending one, so a burst of
//     turns rides one envelope;
//   - the CEILING (the push interval, 120 s by default and lowered by the
//     org's governance pin), which is also the only push that refreshes the
//     governance rails - activity pushes stay light. The backpressure hint
//     never moves the ceiling: a ceiling that falls inside it is a
//     RAIL-ONLY cycle (rails refreshed, no upload; Sol A5-06).
//
// A byte-capped batch pushes again after BacklogGap instead of sleeping.
//
// Pure: no clock, no I/O. Next is an ordered rule table walked top-down; the
// earliest candidate wins and a tie goes to the rail-refreshing ceiling.
package pushsched
