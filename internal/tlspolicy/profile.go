package tlspolicy

import (
	"crypto/tls"
	"fmt"
)

// Profile is the transport key-exchange policy of one controlled hop (#1127,
// DEC-05 and DEC-06 of the post-quantum migration ADR).
//
// Strict is handshake confidentiality only: it requires TLS 1.3 and the hybrid
// X25519MLKEM768 group so a recording adversary cannot later decrypt the
// session. It says nothing about who the peer is. Certificate authentication is
// reported separately and stays classical (a classical chain, or a leaf SPKI pin
// that replaces chain verification) unless a separate check shows otherwise.
type Profile string

const (
	// ProfileCompat is today's behavior, unchanged: TLS 1.2 minimum, Go's default
	// groups, nothing claimed. It is the default and must not silently change.
	ProfileCompat Profile = "compat"
	// ProfileStrict refuses anything weaker before a credential or application
	// byte leaves the process.
	ProfileStrict Profile = "strict"
)

// ParseProfile accepts exactly "compat" or "strict". Case, whitespace and the
// empty string are refused so a typo in a security setting is visible; callers
// own the documented default for an absent value.
func ParseProfile(raw string) (Profile, error) {
	switch p := Profile(raw); p {
	case ProfileCompat, ProfileStrict:
		return p, nil
	}
	return "", fmt.Errorf("TLS profile must be compat or strict, got %q", raw)
}

// Apply mutates cfg in place to enforce the profile. It touches only the
// version bounds, group preferences and resumption state, never certificates,
// roots, server names or verification callbacks, so each hop keeps its own CA,
// hostname and SPKI-pin checks. Compat (and the zero value) change nothing.
// Any other value fails closed to strict rather than weakening.
//
// Strict sets an explicit group list instead of relying on Go's defaults:
// GODEBUG (tlsmlkem) and toolchain defaults change the default list, not an
// explicit one.
func (p Profile) Apply(cfg *tls.Config) {
	if p == ProfileCompat || p == "" {
		return
	}
	cfg.MinVersion = tls.VersionTLS13
	cfg.MaxVersion = tls.VersionTLS13
	cfg.CurvePreferences = []tls.CurveID{tls.X25519MLKEM768}
	cfg.SessionTicketsDisabled = true
	cfg.ClientSessionCache = nil
}

// Hop names a controlled connection for diagnostics. Only hops where both ends
// are Hikyo-controlled may select strict (profiles.md hop table); every other
// hop stays compat and is not represented here.
type Hop string

const (
	HopNativeListener Hop = "native-listener"
	HopCLIClient      Hop = "cli-client"
	HopOperatorClient Hop = "operator-client"
)

// Negotiation is one observed handshake, sanitized to closed vocabularies. It
// carries no peer address, name, certificate or secret. It describes only this
// process's own connection: it says nothing about a proxy or other terminator.
type Negotiation struct {
	Hop     Hop
	Profile Profile
	Version string
	Group   string
	Resumed bool
}

// VersionName maps a TLS version to a closed name.
func VersionName(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "tls1.0"
	case tls.VersionTLS11:
		return "tls1.1"
	case tls.VersionTLS12:
		return "tls1.2"
	case tls.VersionTLS13:
		return "tls1.3"
	}
	return "unknown"
}

// GroupName maps a negotiated group to a closed name. A group Go did not report
// (zero) or this table does not know is "unknown": absence of data is never
// read as classical or as post-quantum.
func GroupName(id tls.CurveID) string {
	switch id {
	case tls.X25519MLKEM768:
		return "x25519mlkem768"
	case tls.X25519:
		return "x25519"
	case tls.CurveP256:
		return "secp256r1"
	case tls.CurveP384:
		return "secp384r1"
	case tls.CurveP521:
		return "secp521r1"
	}
	return "unknown"
}

// Observed builds the sanitized record for a completed handshake.
func Observed(hop Hop, profile Profile, state tls.ConnectionState) Negotiation {
	if profile == "" {
		profile = ProfileCompat
	}
	return Negotiation{Hop: hop, Profile: profile, Version: VersionName(state.Version), Group: GroupName(state.CurveID), Resumed: state.DidResume}
}

// Observe reports each handshake that reaches verification to record, then runs
// any VerifyConnection the config already had. It observes only; it refuses
// nothing and performs no certificate authentication. Post-quantum
// authentication (an ML-DSA chain check) is a separate decision owned by #1133
// and is deliberately absent here.
func Observe(cfg *tls.Config, hop Hop, profile Profile, record func(Negotiation)) {
	next := cfg.VerifyConnection
	cfg.VerifyConnection = func(state tls.ConnectionState) error {
		record(Observed(hop, profile, state))
		if next != nil {
			return next(state)
		}
		return nil
	}
}
