// Package guardtest is the one dry run behind `observer guard test` and the
// Security page's "Test a command" card (POST /api/guard/test): build the
// event a command, file write or raw event would raise, evaluate it with a
// fresh guard engine under the configured mode AND under enforce, and report
// both verdicts. Nothing executes and nothing persists: the engines are
// constructed without a store, a notifier or a policy-state callback.
//
// The callers resolve where the event happens (the CLI from the current
// directory's git root, the dashboard from a project the developer picks) and
// hand Base the cwd and project root; everything after that is shared, so the
// two surfaces cannot disagree on a verdict.
package guardtest
