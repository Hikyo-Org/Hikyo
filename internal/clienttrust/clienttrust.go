// Package clienttrust is the HTTPS trust decision a Hikyo client makes about
// an instance: system CA or an exact leaf SPKI pin (api-cli-surface ADR
// § explicit HTTPS trust). It holds the pure parts only: policy types and
// validation, origin canonicalisation, the SPKI fingerprint, the TLS identity
// probe, the TLS client configuration, and the text of the first-contact
// confirmation. Asking a human to confirm is the caller's job: the CLI uses a
// controlling terminal, the desktop shell a native dialog. Nothing here
// prompts, reads a terminal, or touches the on-disk trust store.
package clienttrust

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/clienterr"
	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
)

// Mode is the explicit HTTPS verification choice. Its zero value is only for
// legacy entries whose JSON omits tls_mode, never an explicit JSON value.
type Mode string

// Profile is the key-exchange policy name a trust entry or bundle records. As
// with Mode, an explicit JSON value must be "compat" or "strict": an empty
// string or null is refused instead of reading as the compat default, which
// only an omitted field means.
type Profile string

const (
	ModeSPKI     = "spki"
	ModeSystemCA = "system-ca"
)

// FieldError is a refusal from a trust field's own UnmarshalJSON.
type FieldError struct{ error }

func (e FieldError) Unwrap() error { return e.error }

// UnmarshalJSON refuses any value other than "spki" or "system-ca".
func (m *Mode) UnmarshalJSON(raw []byte) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return FieldError{fmt.Errorf("TLS verification mode must be spki or system-ca: %w", err)}
	}
	if value != ModeSPKI && value != ModeSystemCA {
		return FieldError{fmt.Errorf("unknown TLS verification mode %q", value)}
	}
	*m = Mode(value)
	return nil
}

// UnmarshalJSON refuses any value other than "compat" or "strict".
func (p *Profile) UnmarshalJSON(raw []byte) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return FieldError{fmt.Errorf("TLS profile must be compat or strict: %w", err)}
	}
	parsed, err := tlspolicy.ParseProfile(value)
	if err != nil {
		return FieldError{err}
	}
	*p = Profile(parsed)
	return nil
}

// Policy is the verification policy an origin was established under.
type Policy struct {
	Origin  string
	Mode    Mode
	Profile Profile
	// SPKIPin is base64(sha256(SubjectPublicKeyInfo)); empty for system-ca.
	SPKIPin string
	// CLISocket carries loopback HTTP over a same-user local socket. It is a
	// CLI transport; the policy only refuses it next to HTTPS.
	CLISocket string
}

// EffectiveMode makes omitted legacy SPKI and explicit SPKI equivalent.
func EffectiveMode(origin string, mode Mode) Mode {
	if strings.HasPrefix(origin, "https://") && mode == "" {
		return ModeSPKI
	}
	return mode
}

// EffectiveProfile makes an omitted profile and an explicit compat equivalent.
func EffectiveProfile(profile Profile) string {
	if profile == "" {
		return string(tlspolicy.ProfileCompat)
	}
	return string(profile)
}

// Validate refuses an inconsistent verification policy.
func (p Policy) Validate() error {
	if p.Profile != "" {
		if _, err := tlspolicy.ParseProfile(string(p.Profile)); err != nil {
			return err
		}
		if p.Profile == Profile(tlspolicy.ProfileStrict) && !strings.HasPrefix(p.Origin, "https://") {
			return errors.New("the strict TLS profile requires an https origin; a local CLI socket is not a TLS connection")
		}
	}
	if p.Mode != "" && p.Mode != ModeSPKI && p.Mode != ModeSystemCA {
		return fmt.Errorf("unknown TLS verification mode %q", p.Mode)
	}
	if !strings.HasPrefix(p.Origin, "https://") {
		if p.Mode != "" {
			return errors.New("TLS verification modes require HTTPS and cannot carry a local CLI socket")
		}
		return nil
	}
	if p.CLISocket != "" {
		return errors.New("https trust cannot carry a local CLI socket")
	}
	if p.Mode == ModeSystemCA && p.SPKIPin != "" {
		return errors.New("system-ca trust cannot carry an SPKI pin")
	}
	return nil
}

// CanonicalOrigin reduces a URL to scheme://host[:port], rejecting anything
// that carries a path, query or credentials — an origin with a path is not an
// origin, and a URL with userinfo is a credential in a place credentials are
// never allowed.
func CanonicalOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", clienterr.Failf(clienterr.ExitUsage, "%q is not a URL: %v", raw, err)
	}
	switch {
	case u.Scheme != "https" && u.Scheme != "http":
		return "", clienterr.Failf(clienterr.ExitUsage, "instance URL must be http or https, got %q", u.Scheme)
	case u.Host == "":
		return "", clienterr.Failf(clienterr.ExitUsage, "instance URL %q has no host", raw)
	case u.User != nil:
		return "", clienterr.Failf(clienterr.ExitUsage, "instance URL must not carry credentials in the URL")
	case u.Path != "" && u.Path != "/":
		return "", clienterr.Failf(clienterr.ExitUsage, "instance URL must be an origin, not a path: %q", raw)
	case u.RawQuery != "" || u.Fragment != "":
		return "", clienterr.Failf(clienterr.ExitUsage, "instance URL must be a bare origin: %q", raw)
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

// IsLoopbackOrigin decides whether an HTTP application origin may be carried
// over an authenticated local socket. url.Hostname
// strips the brackets an IPv6 literal carries, so `http://[::1]:8080` is
// recognised — a hand-rolled colon split reads its host as "[" and refuses a
// perfectly good loopback address.
func IsLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// SPKIFingerprint is base64(sha256(SubjectPublicKeyInfo)) — the same
// construction as HPKP and as `openssl … -pubkey | openssl dgst -sha256`, so
// an operator can compute the expected value without Hikyo's help.
//
// The public key, not the certificate: pinning the certificate would break on
// every renewal, and an operator whose pin breaks quarterly learns to skip
// checking it.
func SPKIFingerprint(cert *x509.Certificate) string {
	return tlspolicy.SPKIFingerprint(cert)
}

// ShortPin abbreviates a pin for display.
func ShortPin(pin string) string {
	if pin == "" {
		return "none"
	}
	if len(pin) <= 16 {
		return pin
	}
	return pin[:16] + "…"
}

// TLSConfig is the client configuration for an https origin: either ordinary
// system-CA verification, or the exact leaf SPKI pin. The profile is applied
// last, so the CA or pin checks stay exactly as the mode defines them.
func TLSConfig(origin string, mode Mode, pin string, profile Profile) *tls.Config {
	var config *tls.Config
	if mode == ModeSystemCA {
		// Ordinary Go TLS verifies the chain, validity and hostname against
		// the system roots. No pin callback or verification bypass applies.
		config = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		config = &tls.Config{
			MinVersion: tls.VersionTLS12,
			// The pin REPLACES chain and hostname verification rather than
			// supplementing it, deliberately: the self-hosted audience this
			// product is built for routinely runs a certificate their own CA
			// issued, and refusing that would push them to a worse workaround.
			// What is left is strictly stronger than a chain for this purpose
			// — an impostor needs the pinned private key, and a
			// publicly-trusted certificate for the same hostname does not
			// become acceptable just because it chains to a public root.
			//
			// The pin is checked against the LEAF ONLY (PeerCertificates[0]).
			// Scanning the whole chain would let an attacker satisfy it by
			// including the legitimate certificate as an intermediate while
			// presenting a leaf whose private key they hold — the pin would
			// match a certificate that is not the one terminating the
			// connection.
			// VerifyConnection, not VerifyPeerCertificate: Go skips the latter
			// on a resumed session, so a session cache would bypass the pin.
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return fmt.Errorf("%s presented no certificate", origin)
				}
				if SPKIFingerprint(cs.PeerCertificates[0]) == pin {
					return nil
				}
				return fmt.Errorf(
					"certificate identity for %s does not match the pin recorded at establishment (%s). "+
						"This is what an interception looks like; it is also what a re-keyed certificate looks like. "+
						"If the change is legitimate, re-establish the instance deliberately",
					origin, ShortPin(pin))
			},
			// Chain verification is intentionally replaced by the exact leaf
			// SPKI check above so operator-issued certificates remain usable.
			// codeql[go/disabled-certificate-check]
			InsecureSkipVerify: true, //nolint:gosec // replaced by the leaf public-key pin above, which a valid chain alone cannot satisfy
		}
	}
	// With HTTPS_PROXY naming an https proxy, the proxy leg shares this
	// config: strict then also refuses a classical-only proxy.
	tlspolicy.Profile(profile).Apply(config)
	return config
}

// InspectIdentity probes only the TLS identity, without application requests
// or credentials. trustMode must be earned before calling: an explicit TLS-mode
// flag for new trust, or human consent for an SPKI-only remote inspection.
// Empty and unknown policies never dial. A later fingerprint confirmation is
// still required before the SPKI result may become trusted.
func InspectIdentity(parent context.Context, origin string, timeout time.Duration, trustMode Mode) (string, error) {
	if trustMode != ModeSPKI && trustMode != ModeSystemCA {
		return "", clienterr.Failf(clienterr.ExitRefused, "TLS identity inspection requires an explicit verification policy, got %q", trustMode)
	}
	u, err := url.Parse(origin)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" {
		return "", clienterr.Failf(clienterr.ExitRefused, "TLS identity inspection requires HTTPS")
	}
	// url.Hostname/Port strip the brackets an IPv6 literal carries in a URL,
	// and net.JoinHostPort puts them back. Splitting on ":" by hand gets
	// `https://[::1]:8443` wrong in three different places.
	port := u.Port()
	if port == "" {
		port = "443"
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	rawConn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return "", clienterr.Failf(clienterr.ExitUnavailable, "cannot reach %s: %v", origin, err)
	}
	config := &tls.Config{
		ServerName: u.Hostname(),
		MinVersion: tls.VersionTLS12,
		// Only explicit SPKI inspection consent permits the unverified
		// fingerprint probe. System-CA inspection always verifies normally.
		// Neither policy sends application requests or credentials here.
		InsecureSkipVerify: trustMode == ModeSPKI,
	}
	conn := tls.Client(rawConn, config)
	if err := conn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return "", clienterr.Failf(clienterr.ExitUnavailable, "cannot reach %s: %v", origin, err)
	}
	defer func() { _ = conn.Close() }()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", clienterr.Failf(clienterr.ExitRefused, "%s presented no certificate", origin)
	}
	return SPKIFingerprint(certs[0]), nil
}

// FirstContactPrompt is the question a human answers before a new instance is
// trusted: origin, verification policy and certificate identity. The CLI asks
// it on a controlling terminal; a GUI shows the same facts in its own dialog.
// socket is the local CLI socket for a loopback instance, empty otherwise.
func FirstContactPrompt(origin string, mode Mode, pin, socket string) string {
	identityLabel, identity := "certificate", ShortPin(pin)
	effective := EffectiveMode(origin, mode)
	if mode == ModeSPKI {
		identity += " (leaf SPKI; no CA chain, certificate validity or hostname verification)"
	}
	if mode == ModeSystemCA {
		identityLabel, identity = "verification", "system trusted CA chain, validity and hostname; certificate keys may rotate"
	}
	displayed := string(effective)
	if socket != "" {
		identityLabel, identity = "local socket", socket+" (same uid required)"
		displayed = "same-user local socket"
	}
	return fmt.Sprintf(
		"Establish trust for a new instance?\n\n    origin:       %s\n    trust mode:   %s\n    %-13s %s\n\nRecord it",
		origin, displayed, identityLabel+":", identity)
}
