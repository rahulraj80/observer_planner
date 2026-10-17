package email

import "github.com/marmutapp/superbased-observer/internal/notifycfg"

// Config is the [email] block. Its shape, validation and credential
// resolution live in internal/notifycfg, a neutral core package, so the node
// and org config loaders embed it without importing this sender package
// (modular packaging phase PB, rev F1). The alias keeps every caller
// unchanged.
type Config = notifycfg.EmailConfig

// TLS mode and auth method names (see internal/notifycfg).
const (
	TLSStartTLS = notifycfg.TLSStartTLS
	TLSImplicit = notifycfg.TLSImplicit
	TLSNone     = notifycfg.TLSNone
	AuthAuto    = notifycfg.AuthAuto
	AuthPlain   = notifycfg.AuthPlain
	AuthLogin   = notifycfg.AuthLogin
)
