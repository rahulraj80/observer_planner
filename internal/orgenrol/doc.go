// Package orgenrol is the pure core of organization enrolment that both the
// `observer enroll` CLI and the node dashboard's Settings > Enrolment page run
// through (backlog item 6: every CLI action must also be doable from the UI).
//
// It owns three things and nothing else:
//
//   - input sanity checks for an enrolment request (server URL shape, the
//     one-time token's compound `<id>.<secret>` shape, the method vocabulary),
//     applied before anything is exchanged or written;
//   - the governance-grant consent screen as DATA (Disclosure): the plain-
//     English rendering of every authority token and the "may / may not"
//     lines. The CLI prints it and the dashboard renders it, so both show
//     the same words for the same grant;
//   - the closed failure vocabulary (Failure / Code) a dashboard handler maps
//     onto HTTP statuses through a table.
//
// It does no I/O: no database/sql, no net/http, no fsnotify (pinned by
// imports_test.go). The exchange itself, the grant write and the config
// writes stay in cmd/observer behind one service seam.
package orgenrol
