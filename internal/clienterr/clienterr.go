// Package clienterr is the client-side exit-code taxonomy and error type
// shared by the CLI and the packages the desktop shell imports. It depends on
// the standard library only, so importing it never pulls in internal/cli.
package clienterr

import (
	"fmt"
	"time"
)

// The closed exit-code set (api-cli-surface ADR § Output grammar). It is
// scoped to Hikyo's own termination, and it is frozen from the first stable
// release: scripts branch on codes, never on parsing error prose.
const (
	// ExitOK is success.
	ExitOK = 0
	// ExitInternal is an unexpected fault.
	ExitInternal = 1
	// ExitUsage is a malformed invocation: unknown verb, missing argument,
	// bad flag.
	ExitUsage = 2
	// ExitAuth is authentication required or failed.
	ExitAuth = 3
	// ExitRefused covers validation, policy, a declined ceremony, a
	// trust-store refusal and an output-grammar refusal — everything the
	// server or the client declined to do.
	ExitRefused = 4
	// ExitNotFound is not found, WHICH IS ALSO UNAUTHORIZED, indistinguishable
	// by design: unauthorized ≡ nonexistent holds on the CLI exactly as it
	// does on the wire.
	ExitNotFound = 5
	// ExitUnavailable is the server or transport being unreachable, or the
	// server failing (5xx).
	ExitUnavailable = 6
	// ExitRateLimited is the server throttling this caller (429): it is up and
	// answering, and asks the caller to come back later. The Retry-After
	// seconds, when the server sent them, are printed on stderr. Split from
	// ExitUnavailable by declared amendment (#806) so a script can tell
	// throttling from an outage without matching message text.
	ExitRateLimited = 7
)

// Error carries an exit code out of a verb.
type Error struct {
	Code int
	Err  error
	// Detail is the API's safe machine-readable detail for a refusal, when the
	// response carried one. Callers match it exactly instead of the message.
	Detail string
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Failf builds an Error with a formatted message.
func Failf(code int, format string, args ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

// RateLimitedError is a throttled response. RetryAfter is the server's
// Retry-After, zero when the response carried no usable value.
type RateLimitedError struct {
	Message    string
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	if e.RetryAfter <= 0 {
		return e.Message
	}
	return fmt.Sprintf("%s; Retry-After: %d", e.Message, int(e.RetryAfter/time.Second))
}
