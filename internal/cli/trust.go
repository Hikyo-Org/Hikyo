package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/Hikyo-Org/hikyo/internal/localsocket"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
)

// The local trust store (api-cli-surface ADR § Context model).
//
// Instance trust is separate from, and prior to, context resolution, and the
// reason is concrete: without it, a malicious repository's pin file plus an
// ambient CI token is a bearer-token exfiltration. The token ships to an
// attacker's validly-certificated HTTPS server before Hikyo authorization is
// ever evaluated — the certificate chain is real, so TLS raises nothing.
//
// An instance therefore enters the store by exactly two acts, neither of
// which repository content can perform:
//
//   - interactive establishment, which requires a terminal and displays the
//     origin and its TLS verification policy for explicit confirmation;
//   - provisioned establishment, where a trust bundle arrives through the
//     same protected channel as the credential itself.
//
// The security argument for the second is alignment of channels: an attacker
// who cannot read the secret store cannot redirect the credential, and one
// who can read it already holds the credential.

// TLSMode is the explicit HTTPS verification choice. Its zero value is only
// for legacy entries whose JSON omits tls_mode, never an explicit JSON value.
type TLSMode string

func (m *TLSMode) UnmarshalJSON(raw []byte) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("TLS verification mode must be spki or system-ca: %w", err)
	}
	if value != TLSModeSPKI && value != TLSModeSystemCA {
		return fmt.Errorf("unknown TLS verification mode %q", value)
	}
	*m = TLSMode(value)
	return nil
}

const (
	TLSModeSPKI     = "spki"
	TLSModeSystemCA = "system-ca"
)

// TrustEntry records the canonical origin and verification policy captured
// at establishment.
type TrustEntry struct {
	// Name is the local reference a pin file or context names. A repository
	// file may name a reference; it can never introduce an origin.
	Name string `json:"name"`
	// Origin is scheme+host+port, canonicalised. A credential is presented
	// only to this origin, and a redirect off it is never followed with one.
	Origin string `json:"origin"`
	// SPKIPin is base64(sha256(SubjectPublicKeyInfo)) of the certificate
	// identity seen at establishment. Empty for explicit system-ca trust or
	// a loopback HTTP origin carried over the same-user Unix socket below.
	SPKIPin string `json:"spki_pin,omitempty"`
	// TLSMode selects HTTPS verification. Omission preserves legacy SPKI trust.
	TLSMode TLSMode `json:"tls_mode,omitempty"`
	// CLISocket carries loopback HTTP without putting credentials on an
	// identity-free TCP listener. Its directory and socket are euid-owned 0700
	// and 0600, and both peers verify the other's kernel-reported UID.
	CLISocket string `json:"cli_socket,omitempty"`
}

// TrustBundle is the provisioned-establishment file: the CI path's import
// format. It is deliberately the same shape as a store entry — trust material
// is not a secret, it is a binding.
type TrustBundle struct {
	Name      string  `json:"name"`
	Origin    string  `json:"origin"`
	SPKIPin   string  `json:"spki_pin,omitempty"`
	TLSMode   TLSMode `json:"tls_mode,omitempty"`
	CLISocket string  `json:"cli_socket,omitempty"`
}

// ErrUntrusted reports an instance reference that is not in the local store.
// The CLI names the missing provisioning step and stops; it never
// prompts-to-trust mid-command, because a prompt in the middle of a scripted
// run is a prompt nobody reads.
var ErrUntrusted = errors.New("instance is not in the local trust store")

// TrustStore is the on-disk store under the XDG state directory.
type TrustStore struct {
	dir string
}

func (s *TrustStore) path() string { return filepath.Join(s.dir, "trust.json") }

// Load reads the store, returning an empty one when it does not exist yet.
func (s *TrustStore) Load() (map[string]TrustEntry, error) {
	raw, err := readTrustFile(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]TrustEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries map[string]TrustEntry
	if err := decodeTrustJSON(raw, &entries); err != nil {
		return nil, fmt.Errorf("trust store at %s is unreadable: %w", s.path(), err)
	}
	if entries == nil {
		entries = map[string]TrustEntry{}
	}
	for name, entry := range entries {
		if err := validateTrustEntry(name, entry); err != nil {
			return nil, fmt.Errorf("trust store at %s contains an invalid entry %q: %w", s.path(), name, err)
		}
	}
	return entries, nil
}

func validateTrustEntry(name string, entry TrustEntry) error {
	if name == "" || entry.Name != name || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return errors.New("entry name must be nonempty, control-free, and match its map key")
	}
	origin, err := CanonicalOrigin(entry.Origin)
	if err != nil || origin != entry.Origin {
		return errors.New("origin must be canonical")
	}
	if err := validateTLSMode(entry); err != nil {
		return err
	}
	if strings.HasPrefix(origin, "https://") {
		if entry.TLSMode == TLSModeSystemCA {
			return nil
		}
		if entry.CLISocket != "" {
			return errors.New("https trust cannot carry a local CLI socket")
		}
		decoded, err := base64.StdEncoding.DecodeString(entry.SPKIPin)
		if err != nil || len(decoded) != 32 {
			return errors.New("https origin requires a base64 SHA-256 SPKI pin")
		}
		return nil
	}
	if !isLoopbackOrigin(origin) || entry.SPKIPin != "" {
		return errors.New("http trust is allowed only for loopback and cannot carry an SPKI pin")
	}
	// Legacy loopback entries remain readable so an upgrade can name the
	// remediation instead of making the whole trust store unreadable. NewClient
	// still refuses to carry credentials until a socket is established.
	if entry.CLISocket != "" {
		if err := localsocket.ValidatePath(entry.CLISocket); err != nil {
			return fmt.Errorf("local CLI socket: %w", err)
		}
	}
	return nil
}

// effectiveTLSMode makes omitted legacy SPKI and explicit SPKI equivalent.
func effectiveTLSMode(entry TrustEntry) TLSMode {
	if strings.HasPrefix(entry.Origin, "https://") && entry.TLSMode == "" {
		return TLSModeSPKI
	}
	return entry.TLSMode
}

func validateTLSMode(entry TrustEntry) error {
	if entry.TLSMode != "" && entry.TLSMode != TLSModeSPKI && entry.TLSMode != TLSModeSystemCA {
		return fmt.Errorf("unknown TLS verification mode %q", entry.TLSMode)
	}
	if !strings.HasPrefix(entry.Origin, "https://") {
		if entry.TLSMode != "" {
			return errors.New("TLS verification modes require HTTPS and cannot carry a local CLI socket")
		}
		return nil
	}
	if entry.CLISocket != "" {
		return errors.New("https trust cannot carry a local CLI socket")
	}
	if entry.TLSMode == TLSModeSystemCA && entry.SPKIPin != "" {
		return errors.New("system-ca trust cannot carry an SPKI pin")
	}
	return nil
}

func validateCredentialTransport(entry TrustEntry) error {
	if err := validateTLSMode(entry); err != nil {
		return err
	}
	if strings.HasPrefix(entry.Origin, "https://") {
		if entry.CLISocket != "" {
			return errors.New("https trust cannot carry a local CLI socket")
		}
		return nil
	}
	if !isLoopbackOrigin(entry.Origin) {
		return errors.New("plaintext http is allowed only for loopback over an authenticated local CLI socket")
	}
	if entry.CLISocket == "" {
		return errors.New("loopback http trust requires an authenticated local CLI socket")
	}
	if err := localsocket.ValidatePath(entry.CLISocket); err != nil {
		return fmt.Errorf("local CLI socket: %w", err)
	}
	return nil
}

// Lookup resolves a reference. A missing reference is ErrUntrusted, never a
// silent trust-on-first-use.
func (s *TrustStore) Lookup(name string) (TrustEntry, error) {
	entries, err := s.Load()
	if err != nil {
		return TrustEntry{}, err
	}
	e, ok := entries[name]
	if !ok {
		// Exit 4, refused: a trust-store refusal is a policy decision this
		// client made, not an internal fault and not a missing object. The
		// exit-code matrix pins that so a script can tell the difference.
		return TrustEntry{}, &Error{Code: ExitRefused, Err: fmt.Errorf(
			"%w: %q. Establish it interactively with `hikyo login <url>` or `hikyo context create --instance <url>`, "+
				"or provision it with --trust-file / HIKYO_TRUST_BUNDLE through the same protected channel as the credential",
			ErrUntrusted, name)}
	}
	return e, nil
}

// Put records an entry. Rewriting an existing origin or pin is refused: a
// silent re-pin is indistinguishable from the attack the pin exists to stop,
// so changing one is a delete-then-establish, done deliberately.
func (s *TrustStore) Put(e TrustEntry) error {
	if err := validateTrustEntry(e.Name, e); err != nil {
		return failf(ExitRefused, "invalid trust entry %q: %v", e.Name, err)
	}
	unlock, err := lockStateDir(s.dir)
	if err != nil {
		return err
	}
	defer unlock()
	entries, err := s.Load()
	if err != nil {
		return err
	}
	if prior, ok := entries[e.Name]; ok {
		if prior.Origin != e.Origin || effectiveTLSMode(prior) != effectiveTLSMode(e) || prior.SPKIPin != e.SPKIPin || prior.CLISocket != e.CLISocket {
			return failf(ExitRefused,
				"instance %q is already established with a different identity\n"+
					"  recorded: %s (mode %s, pin %s)\n"+
					"  offered:  %s (mode %s, pin %s)\n"+
					"A changed origin, mode or identity requires deliberate re-establishment. If the change is legitimate, "+
					"remove the entry deliberately with `hikyo context delete --instance %s` and establish it again.",
				e.Name, prior.Origin, effectiveTLSMode(prior), shortPin(prior.SPKIPin), e.Origin, effectiveTLSMode(e), shortPin(e.SPKIPin), e.Name)
		}
		return nil
	}
	entries[e.Name] = e
	return s.write(entries)
}

// Delete removes an entry.
func (s *TrustStore) Delete(name string) error {
	unlock, err := lockStateDir(s.dir)
	if err != nil {
		return err
	}
	defer unlock()
	entries, err := s.Load()
	if err != nil {
		return err
	}
	if _, ok := entries[name]; !ok {
		return failf(ExitNotFound, "no trust-store entry named %q", name)
	}
	delete(entries, name)
	return s.write(entries)
}

func (s *TrustStore) write(entries map[string]TrustEntry) error {
	if err := ensureTrustStateDir(s.dir); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return securefile.WriteAtomic(s.path(), append(raw, '\n'), 0o600)
}

// CanonicalOrigin reduces a URL to scheme://host[:port], rejecting anything
// that carries a path, query or credentials — an origin with a path is not an
// origin, and a URL with userinfo is a credential in a place credentials are
// never allowed.
func CanonicalOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", failf(ExitUsage, "%q is not a URL: %v", raw, err)
	}
	switch {
	case u.Scheme != "https" && u.Scheme != "http":
		return "", failf(ExitUsage, "instance URL must be http or https, got %q", u.Scheme)
	case u.Host == "":
		return "", failf(ExitUsage, "instance URL %q has no host", raw)
	case u.User != nil:
		return "", failf(ExitUsage, "instance URL must not carry credentials in the URL")
	case u.Path != "" && u.Path != "/":
		return "", failf(ExitUsage, "instance URL must be an origin, not a path: %q", raw)
	case u.RawQuery != "" || u.Fragment != "":
		return "", failf(ExitUsage, "instance URL must be a bare origin: %q", raw)
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
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

// inspectTLSIdentity probes only the TLS identity, without application requests
// or credentials. trustMode must be earned before calling: an explicit TLS-mode
// flag for new trust, or terminal consent for an SPKI-only remote inspection.
// Empty and unknown policies never dial. A later fingerprint confirmation is
// still required before the SPKI result may become trusted.
func inspectTLSIdentity(parent context.Context, origin string, timeout time.Duration, trustMode TLSMode) (string, error) {
	if trustMode != TLSModeSPKI && trustMode != TLSModeSystemCA {
		return "", failf(ExitRefused, "TLS identity inspection requires an explicit verification policy, got %q", trustMode)
	}
	u, err := url.Parse(origin)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" {
		return "", failf(ExitRefused, "TLS identity inspection requires HTTPS")
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
		return "", failf(ExitUnavailable, "cannot reach %s: %v", origin, err)
	}
	config := &tls.Config{
		ServerName: u.Hostname(),
		MinVersion: tls.VersionTLS12,
		// Only explicit SPKI inspection consent permits the unverified
		// fingerprint probe. System-CA inspection always verifies normally.
		// Neither policy sends application requests or credentials here.
		InsecureSkipVerify: trustMode == TLSModeSPKI,
	}
	conn := tls.Client(rawConn, config)
	if err := conn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return "", failf(ExitUnavailable, "cannot reach %s: %v", origin, err)
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", failf(ExitRefused, "%s presented no certificate", origin)
	}
	return SPKIFingerprint(certs[0]), nil
}

func shortPin(pin string) string {
	if pin == "" {
		return "none"
	}
	if len(pin) <= 16 {
		return pin
	}
	return pin[:16] + "…"
}

// Trust schemas reject unsupported fields instead of silently discarding a
// requested verification policy or accepting a misspelled security field.
func decodeTrustJSON(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}
