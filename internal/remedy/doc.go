// Package remedy is the ONE owner of how a health finding's fix is spelled
// for each audience (T-421 item 3). A finding carries a structured
// Remediation (a closed action id plus a tool parameter); the CLI renders it
// as the command (Command), the dashboard Health page as a button linking to
// the control that does the same thing (ControlFor). Neither audience keeps
// its own copy of the string.
//
// Pure: no I/O, no SQL, no HTTP, no observer-internal imports. The table is
// data, walked by action id.
package remedy
