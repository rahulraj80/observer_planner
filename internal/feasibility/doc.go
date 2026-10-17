// Package feasibility is the one pure seam that answers "can this choice be
// honoured here, and if not, why and where do I change it" (operator ruling
// D-514, lane UX-INFEASIBLE, T-720).
//
// The rule it encodes: a setting that this build or this deployment cannot
// honour never stops the node or the server from starting. It starts, leaves
// that feature off and surfaces ONE clear reason. Only a choice whose absence
// would be unsafe (security, data loss, data written somewhere other than
// where it lives) refuses, and the refusal still names the cause and the fix.
// Every chooser (setup wizards, Settings > Components, the web2 node
// components composer, observer init's checklist, the deploy planner, the
// New Terminal picker) shows an infeasible option disabled with the same
// reason and the step to go back to.
//
// Shape: a side (node or org server) owns an ordered table of Rule rows, one
// per choosable option, each naming what the option needs (component ids or
// host facts) and the other options it needs or conflicts with. The caller
// fills an Input from what it knows (which options the config turns on, and
// why each need cannot be met here) and Evaluate walks the table top-down,
// returning one Verdict per row: {feasible, reason, fix, fix_step} plus the
// Effect the caller applies (none, off, refuse, unavailable). The package
// does no I/O and imports only the standard library, so the CLI, the
// daemon / server boot, doctor and every HTTP handler call the same code and
// can never disagree.
package feasibility
