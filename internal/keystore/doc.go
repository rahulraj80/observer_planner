// Package keystore is the shared, keychain-first, fail-closed named-secret
// store. It was extracted from internal/cloudcred so that every node module
// that holds a credential (personal cloud sign-in, Plans & limits provider
// keys) uses ONE hardened implementation instead of a copy.
//
// # Backends
//
// Open probes the OS keychain (github.com/zalando/go-keyring: macOS
// Keychain, libsecret / Secret Service, Windows Credential Manager) once with
// a throwaway record. On success every secret lives in the keychain under the
// caller's service name, with OS-backed encryption at rest.
//
// When the keychain is unavailable (headless Linux with no Secret Service, a
// CIFS-mounted HOME) Open falls back to a hardened 0600 file store, but ONLY
// on a platform build whose file protections are real (FileFallbackSecure;
// unix today). Every read and write of that store:
//
//   - validates the private directory (0700, owned by this user, not a
//     symlink);
//   - refuses a symlinked file (Lstat plus O_NOFOLLOW) and re-checks the open
//     handle to close the Lstat-to-open race;
//   - verifies POSIX ownership on the path and the open handle;
//   - writes atomically (unique temp file, fsync, rename, directory fsync).
//
// On every other platform build (Windows today) there is no verified DACL or
// reparse-point check, so Open fails closed: every call returns
// ErrInsecureFallbackUnavailable rather than writing a plaintext secret to an
// unverified file.
//
// # Names
//
// A secret name is validated before it is used as a keychain key or a file
// name suffix: non-empty, at most 200 bytes, only [A-Za-z0-9._@:-], and never
// "." or "..". Values are capped at MaxValueBytes.
//
// # Purity
//
// stdlib plus github.com/zalando/go-keyring only (pinned by imports_test.go).
// No internal package, no database/sql, net, net/http or fsnotify.
package keystore
