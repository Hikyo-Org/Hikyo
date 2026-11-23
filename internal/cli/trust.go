package cli

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Hikyo-Org/hikyo/internal/clienttrust"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/localsocket"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
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

// TLSMode is the explicit HTTPS verification choice and TLSProfile the
// key-exchange policy name; both live in internal/clienttrust so the desktop
// shell shares them without importing this package.
type (
	TLSMode    = clienttrust.Mode
	TLSProfile = clienttrust.Profile
)

const (
	TLSModeSPKI     = clienttrust.ModeSPKI
	TLSModeSystemCA = clienttrust.ModeSystemCA
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
	// TLSProfile selects the key-exchange policy for this HTTPS origin (#1127):
	// omitted or "compat" is today's behavior; "strict" requires TLS 1.3 and the
	// hybrid X25519MLKEM768 group and fails before the bearer leaves the process.
	// It protects handshake confidentiality only. Authentication stays whatever
	// TLSMode says, which is classical; an SPKI pin gives no authenticity claim.
	// An established entry's profile changes only by deliberate re-establishment.
	TLSProfile TLSProfile `json:"tls_profile,omitempty"`
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
	// TLSProfile is carried into the entry unchanged; see TrustEntry.TLSProfile.
	TLSProfile TLSProfile `json:"tls_profile,omitempty"`
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
func (s *TrustStore) Load() (map[string]TrustEntry, error) { return s.load(true) }

// load validates every entry. checkConflicts additionally refuses equivalent
// origins with different profiles; only Delete skips it, so the step that
// refusal tells the user to run can repair the store it refuses to load.
func (s *TrustStore) load(checkConflicts bool) (map[string]TrustEntry, error) {
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
	if checkConflicts {
		if err := refuseConflictingProfiles(entries); err != nil {
			return nil, fmt.Errorf("trust store at %s: %w", s.path(), err)
		}
	}
	return entries, nil
}

// refuseConflictingProfiles keeps a store written before Put enforced this from
// resolving an origin to whichever of two entries a caller reaches first. A
// store whose entries all share one effective profile (every store without a
// strict entry) loads exactly as before.
func refuseConflictingProfiles(entries map[string]TrustEntry) error {
	for _, a := range slices.Sorted(maps.Keys(entries)) {
		for _, b := range slices.Sorted(maps.Keys(entries)) {
			if a < b && sameOrigin(entries[a].Origin, entries[b].Origin) && effectiveTLSProfile(entries[a]) != effectiveTLSProfile(entries[b]) {
				return fmt.Errorf("entries %q and %q name the same origin with different TLS profiles; delete one with `hikyo context delete --instance <name>` and establish the intended profile again", a, b)
			}
		}
	}
	return nil
}

// originKey is the comparison form of a canonical origin: names the same
// server when only spelling differs. The scheme's default port, a trailing host
// dot, leading zeros in the port and the textual form of an IP literal do not
// name a different server. CanonicalOrigin itself is unchanged because stored
// entries keep their exact spelling.
func originKey(origin string) string {
	u, err := url.Parse(origin)
	if err != nil {
		return origin
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if addr, err := netip.ParseAddr(host); err == nil {
		host = addr.Unmap().String()
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	if n, err := strconv.ParseUint(port, 10, 16); err == nil {
		port = strconv.FormatUint(n, 10)
	}
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host
}

func sameOrigin(a, b string) bool { return originKey(a) == originKey(b) }

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

func samePolicy(a, b TrustEntry) bool {
	return effectiveTLSMode(a) == effectiveTLSMode(b) && a.SPKIPin == b.SPKIPin && a.CLISocket == b.CLISocket && effectiveTLSProfile(a) == effectiveTLSProfile(b)
}

// effectiveTLSProfile makes an omitted profile and an explicit compat equivalent.
func effectiveTLSProfile(entry TrustEntry) string {
	return clienttrust.EffectiveProfile(entry.TLSProfile)
}

// effectiveTLSMode makes omitted legacy SPKI and explicit SPKI equivalent.
func effectiveTLSMode(entry TrustEntry) TLSMode {
	return clienttrust.EffectiveMode(entry.Origin, entry.TLSMode)
}

func validateTLSMode(entry TrustEntry) error {
	return clienttrust.Policy{Origin: entry.Origin, Mode: entry.TLSMode, Profile: entry.TLSProfile, SPKIPin: entry.SPKIPin, CLISocket: entry.CLISocket}.Validate()
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
	// The profile binds to a name, so a second name for the same origin with a
	// different profile would leave the choice between them to chance.
	for name, other := range entries {
		if name != e.Name && sameOrigin(other.Origin, e.Origin) && effectiveTLSProfile(other) != effectiveTLSProfile(e) {
			return failf(ExitRefused,
				"origin %s is already established as %q with a different TLS profile; remove that entry deliberately before establishing %q",
				e.Origin, name, e.Name)
		}
	}
	if prior, ok := entries[e.Name]; ok {
		if prior.Origin != e.Origin || effectiveTLSMode(prior) != effectiveTLSMode(e) || prior.SPKIPin != e.SPKIPin || prior.CLISocket != e.CLISocket || effectiveTLSProfile(prior) != effectiveTLSProfile(e) {
			return failf(ExitRefused,
				"instance %q is already established with a different identity\n"+
					"  recorded: %s (mode %s, pin %s)\n"+
					"  offered:  %s (mode %s, pin %s)\n"+
					"A changed origin, mode, TLS profile or identity requires deliberate re-establishment. If the change is legitimate, "+
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
	entries, err := s.load(false)
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
	raw, err := json.Marshal(entries, jsonwire.Stable, jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	return securefile.WriteAtomic(s.path(), append(raw, '\n'), 0o600)
}

// CanonicalOrigin reduces a URL to scheme://host[:port]; see clienttrust.
func CanonicalOrigin(raw string) (string, error) { return clienttrust.CanonicalOrigin(raw) }

// SPKIFingerprint is base64(sha256(SubjectPublicKeyInfo)); see clienttrust.
func SPKIFingerprint(cert *x509.Certificate) string { return clienttrust.SPKIFingerprint(cert) }

func inspectTLSIdentity(parent context.Context, origin string, timeout time.Duration, trustMode TLSMode) (string, error) {
	return clienttrust.InspectIdentity(parent, origin, timeout, trustMode)
}

func shortPin(pin string) string { return clienttrust.ShortPin(pin) }

// Trust schemas reject unsupported fields instead of silently discarding a
// requested verification policy or accepting a misspelled security field.
func decodeTrustJSON(raw []byte, out any) error {
	err := decodeStrictDocument(bytes.NewReader(raw), out)
	if errors.Is(err, jsonwire.ErrTrailing) {
		return errors.New("expected one JSON object")
	}
	// A field's own refusal reads as it did under encoding/json (v1), without
	// v2's wrapper naming the Go type and JSON pointer.
	var refusal trustFieldError
	if errors.As(err, &refusal) {
		return refusal
	}
	return err
}

// trustFieldError is a refusal from a trust field's own UnmarshalJSON.
type trustFieldError = clienttrust.FieldError
