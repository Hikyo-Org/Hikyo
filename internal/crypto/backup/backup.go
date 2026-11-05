// Package backup owns the age container around an instance export (#76,
// encryption-model ADR § Backups and exports). It is the SOLE importer of
// filippo.io/age in the module, enforced by internal/boundary.
//
// Hikyo specifies no container format of its own: age supplies the recipient
// stanzas, the scrypt passphrase recipient, and the STREAM chunked payload
// with its chunk ordering and truncation detection. What this package owns is
// the contract AROUND the container, which delegation does not supply:
//
//   - a zero-recipient export is refused, loudly, never written in the clear;
//   - an scrypt (passphrase) stanza must be the ONLY stanza in its container,
//     so export takes public recipients or a passphrase and never both — and
//     a container that mixes them is refused on OPEN too, because age enforces
//     that rule only on its own ScryptIdentity path (an X25519 identity would
//     otherwise happily open a container whose passphrase half is a second,
//     weaker door);
//   - the whole archive authenticates through to its final chunk BEFORE any
//     caller applies a byte of it.
//
// The package is a leaf, like the rest of internal/crypto: it imports nothing
// under internal/.
package backup

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"filippo.io/age"
)

const (
	maxHeaderBytes = 256 << 10
	maxRecipients  = 256

	// MaxHybridRecipients is the most hybrid recipients one export accepts. A
	// hybrid recipient is 1959 characters, and the comma-joined configuration
	// value is capped at 65536 bytes (schema.MaxValueBytes): 32 fit
	// (62,719 bytes), 64 do not. It is deliberately lower than the 256-stanza
	// reader bound, which stays as it was.
	MaxHybridRecipients = 32
	// MaxRecipientListBytes mirrors the managed configuration value cap. This
	// leaf package cannot import internal/schema; a test in the importing
	// layers pins the two together.
	MaxRecipientListBytes = 65536
	// maxRecipientChars and maxIdentityChars bound one key string before any
	// bech32 work. A hybrid recipient is 1959 characters, an identity 77.
	maxRecipientChars = 2048
	maxIdentityChars  = 256

	hybridRecipientPrefix = "age1pq1"
	hybridIdentityPrefix  = "AGE-SECRET-KEY-PQ-1"
	hybridStanzaType      = "mlkem768x25519"
	// HPKE encapsulation (1088 + 32) and wrapped 16-byte file key (16 + 16).
	hybridEncapsulationBytes = 1120
	hybridWrappedKeyBytes    = 32
	// Match age's existing export default: 256 MiB, instead of allowing
	// unauthenticated headers to request logN=22 and allocate 4 GiB.
	maxScryptWorkFactor = 18
)

// Export and open refusals. Each is its own sentinel because the operator
// error behind each is different, and a restore runbook that cannot tell
// "you handed me the wrong key" from "this file is a prefix" is useless.
var (
	// ErrNoRecipients is encryption-ADR startup refusal 6: an export with
	// zero configured recipients is refused rather than silently written
	// without encryption.
	ErrNoRecipients = errors.New("backup: export needs at least one age recipient — refusing to write an unencrypted export")

	// ErrRecipientExclusive is the scrypt-stanza exclusivity rule at export.
	ErrRecipientExclusive = errors.New("backup: a passphrase (scrypt) recipient must be the only recipient in its container — export takes public recipients or a passphrase, never both")

	// ErrMixedStanzas is the same rule at open: a container carrying an
	// scrypt stanza alongside any other recipient stanza is refused before
	// any key is tried.
	ErrMixedStanzas = errors.New("backup: container mixes an scrypt stanza with other recipient stanzas — refused per the age specification")

	// ErrTruncated reports a container whose payload ended before a verified
	// final chunk. This is the failure a stream-and-apply restore would
	// commit a prefix of.
	ErrTruncated = errors.New("backup: container ends before its final chunk — truncated or incomplete")

	// ErrUnlock reports an unusable unlock: neither an identity nor a
	// passphrase, or both at once.
	ErrUnlock = errors.New("backup: restore needs exactly one of the backup identity or the container passphrase")

	ErrHeaderTooLarge    = errors.New("backup: container header exceeds the 256 KiB limit")
	ErrTooManyRecipients = errors.New("backup: container exceeds the 256-recipient limit")

	// ErrMixedRecipients refuses a recipient set holding a hybrid recipient
	// beside a classical one: the classical door would open the object alone.
	ErrMixedRecipients = errors.New("backup: hybrid post-quantum and classical recipients cannot be mixed in one export")
	// ErrTooManyHybridRecipients is the deterministic count bound for hybrid sets.
	ErrTooManyHybridRecipients = errors.New("backup: an export accepts at most 32 hybrid recipients")
	// ErrRecipientsTooLarge is the joined-size bound of the configured list.
	ErrRecipientsTooLarge = errors.New("backup: the recipient list exceeds the 65536-byte configuration value limit")
	// ErrDuplicateRecipient refuses a hybrid recipient listed twice.
	ErrDuplicateRecipient = errors.New("backup: duplicate hybrid recipient")
	// ErrMixedHybridStanzas is the reader rule: a hybrid stanza never sits
	// beside a classical, scrypt, ssh, plugin or unknown stanza. age's decrypt
	// path has no label check, so this refusal binds Hikyo readers only.
	ErrMixedHybridStanzas = errors.New("backup: container mixes a hybrid stanza with other recipient stanzas, refused")
	// ErrMalformedHybridStanza refuses a hybrid stanza whose shape is not the
	// single-argument, 32-byte-body form.
	ErrMalformedHybridStanza = errors.New("backup: malformed hybrid recipient stanza")
)

// Options is the export recipient policy. Recipients are age public
// recipients (`age1…`); Passphrase selects age's scrypt recipient. The two
// are mutually exclusive and at least one is required.
type Options struct {
	Recipients []string
	Passphrase string
}

// Unlock is the restore-side secret: exactly one of the two. The instance
// stores neither — the backup identity never touches the datastore, and it is
// deliberately a different failure domain from the root key.
type Unlock struct {
	Identity   string
	Passphrase string
}

// Validate resolves the recipient policy without producing a container, so a
// caller can refuse a bad policy before it creates the file the refusal would
// then have to delete.
func (o Options) Validate() error {
	_, err := o.recipients()
	return err
}

func (o Options) recipients() ([]age.Recipient, error) {
	if len(o.Recipients) > maxRecipients {
		return nil, ErrTooManyRecipients
	}
	if len(o.Recipients) > 0 && o.Passphrase != "" {
		return nil, ErrRecipientExclusive
	}
	if o.Passphrase != "" {
		r, err := age.NewScryptRecipient(o.Passphrase)
		if err != nil {
			return nil, fmt.Errorf("backup: passphrase recipient: %w", err)
		}
		r.SetWorkFactor(maxScryptWorkFactor)
		return []age.Recipient{r}, nil
	}
	if len(o.Recipients) == 0 {
		return nil, ErrNoRecipients
	}
	// Count hybrid recipients by prefix before parsing any of them, so the
	// bound refuses without paying for 256 key parses.
	var hybridPrefixed, listBytes int
	for _, s := range o.Recipients {
		listBytes += len(s) + 1
		if isHybridRecipient(s) {
			hybridPrefixed++
		}
	}
	if hybridPrefixed > MaxHybridRecipients {
		return nil, ErrTooManyHybridRecipients
	}
	if hybridPrefixed > 0 && listBytes-1 > MaxRecipientListBytes {
		return nil, ErrRecipientsTooLarge
	}
	out := make([]age.Recipient, 0, len(o.Recipients))
	seen := make(map[string]bool, hybridPrefixed)
	hybrid := 0
	for i, s := range o.Recipients {
		r, err := parseRecipient(i, s)
		if err != nil {
			return nil, err
		}
		if h, ok := r.(*age.HybridRecipient); ok {
			hybrid++
			if seen[h.String()] {
				return nil, ErrDuplicateRecipient
			}
			seen[h.String()] = true
		}
		out = append(out, r)
	}
	if hybrid > 0 && hybrid != len(out) {
		return nil, ErrMixedRecipients
	}
	return out, nil
}

// IsPolicyError reports whether err is one of the recipient-set policy
// refusals (mixed classes, count, joined size, duplicate). It is the single
// list callers use to decide an error may be shown by name; a malformed key
// error is not on it, because that message must stay generic.
func IsPolicyError(err error) bool {
	for _, policy := range []error{ErrMixedRecipients, ErrTooManyHybridRecipients, ErrRecipientsTooLarge, ErrDuplicateRecipient, ErrTooManyRecipients} {
		if errors.Is(err, policy) {
			return true
		}
	}
	return false
}

// IsHybridRecipient reports whether s selects age's hybrid recipient type. It
// looks at the prefix only; parsing is Options.Validate's job.
func IsHybridRecipient(s string) bool { return isHybridRecipient(s) }

// AnyHybridRecipient reports whether a configured recipient list selects hybrid
// backups, which is what makes the list a hybrid policy.
func AnyHybridRecipient(recipients []string) bool {
	for _, r := range recipients {
		if isHybridRecipient(r) {
			return true
		}
	}
	return false
}

func isHybridRecipient(s string) bool {
	s = strings.TrimSpace(s)
	return len(s) >= len(hybridRecipientPrefix) && strings.EqualFold(s[:len(hybridRecipientPrefix)], hybridRecipientPrefix)
}

// parseRecipient dispatches on the recipient prefix and hands the native age
// type to age unwrapped: no wrapper may hide the recipient's labels, because
// age's label check is what refuses a classical door beside a hybrid one. The
// error names the position, never the value: an operator can paste a private
// identity where a recipient belongs.
func parseRecipient(index int, s string) (age.Recipient, error) {
	s = strings.TrimSpace(s)
	invalid := fmt.Errorf("backup: recipient #%d is not a valid age public recipient", index+1)
	if len(s) > maxRecipientChars {
		return nil, invalid
	}
	if isHybridRecipient(s) {
		r, err := age.ParseHybridRecipient(s)
		if err != nil {
			return nil, invalid
		}
		return r, nil
	}
	r, err := age.ParseX25519Recipient(s)
	if err != nil {
		return nil, invalid
	}
	return r, nil
}

// parseIdentity dispatches on the identity prefix. Neither error echoes the
// identity.
func parseIdentity(s string) (age.Identity, error) {
	s = strings.TrimSpace(s)
	invalid := errors.New("backup: backup identity is not a valid age identity")
	if len(s) > maxIdentityChars {
		return nil, invalid
	}
	if len(s) >= len(hybridIdentityPrefix) && strings.EqualFold(s[:len(hybridIdentityPrefix)], hybridIdentityPrefix) {
		i, err := age.ParseHybridIdentity(s)
		if err != nil {
			return nil, invalid
		}
		return i, nil
	}
	i, err := age.ParseX25519Identity(s)
	if err != nil {
		return nil, invalid
	}
	return i, nil
}

func (u Unlock) identity() (age.Identity, error) {
	switch {
	case (u.Identity == "") == (u.Passphrase == ""):
		return nil, ErrUnlock
	case u.Passphrase != "":
		i, err := age.NewScryptIdentity(u.Passphrase)
		if err != nil {
			return nil, fmt.Errorf("backup: passphrase identity: %w", err)
		}
		i.SetMaxWorkFactor(maxScryptWorkFactor)
		return i, nil
	default:
		// The identity itself never reaches an error string.
		return parseIdentity(u.Identity)
	}
}

// GenerateIdentity mints a fresh age X25519 identity, returning the private
// identity and its public recipient in that order. The private half is the
// operator's to escrow in a custody store SEPARATE from the root key's: two
// keys in one password manager is one failure domain wearing two names.
func GenerateIdentity() (identity, recipient string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", fmt.Errorf("backup: generate identity: %w", err)
	}
	return id.String(), id.Recipient().String(), nil
}

// GenerateHybridIdentity mints a fresh age hybrid identity (X25519 + ML-KEM-768)
// from a 32-byte seed in age's native text form. It is the opt-in sibling of
// GenerateIdentity; nothing selects it by default.
func GenerateHybridIdentity() (identity, recipient string, err error) {
	id, err := age.GenerateHybridIdentity()
	if err != nil {
		return "", "", fmt.Errorf("backup: generate hybrid identity: %w", err)
	}
	return id.String(), id.Recipient().String(), nil
}

// RecipientOf returns the canonical public recipient of a stored age identity,
// X25519 or hybrid, so custody code can validate an escrowed identity and
// publish its recipient without touching age directly.
func RecipientOf(identity string) (string, error) {
	id, err := parseIdentity(identity)
	if err != nil {
		return "", err
	}
	switch id := id.(type) {
	case *age.HybridIdentity:
		return id.Recipient().String(), nil
	case *age.X25519Identity:
		return id.Recipient().String(), nil
	}
	return "", errors.New("backup: backup identity is not a valid age identity")
}

// Encrypt opens the container. The caller writes the archive and MUST Close
// the returned writer: age's final chunk — the thing that distinguishes a
// complete archive from a prefix — is written by Close and by nothing else.
func Encrypt(dst io.Writer, o Options) (io.WriteCloser, error) {
	recipients, err := o.recipients()
	if err != nil {
		return nil, err
	}
	w, err := age.Encrypt(dst, recipients...)
	if err != nil {
		return nil, fmt.Errorf("backup: open container: %w", err)
	}
	return w, nil
}

// ExtractTo decrypts src into dst, verifying the container through to its
// final chunk. It returns only after the WHOLE archive has authenticated, so
// a caller that applies dst's contents only on a nil error cannot commit a
// prefix of a truncated backup.
//
// On error the contents of dst are undefined and must not be applied — that
// is the entire reason dst is a rewindable sink (a temp file) at every call
// site and never the restore applier itself.
func ExtractTo(dst io.Writer, src io.Reader, u Unlock) error {
	identity, err := u.identity()
	if err != nil {
		return err
	}

	// The stanza scan runs before any key is tried, so a mixed container is
	// refused on its shape rather than on whether this caller happens to hold
	// the stronger of its two doors.
	var consumed bytes.Buffer
	header, err := age.ExtractHeader(io.TeeReader(io.LimitReader(src, maxHeaderBytes+1), &consumed))
	if consumed.Len() > maxHeaderBytes {
		return ErrHeaderTooLarge
	}
	if err != nil {
		return fmt.Errorf("backup: read container header: %w", err)
	}
	if err := checkStanzaExclusivity(header); err != nil {
		return err
	}

	r, err := age.Decrypt(io.MultiReader(&consumed, src), identity)
	if err != nil {
		// A container that ends inside the payload's STREAM nonce never
		// reaches a chunk: age fails here, and it is the same truncation.
		return truncationOr("backup: open container", err)
	}
	if _, err := io.Copy(dst, r); err != nil {
		return truncationOr("backup: container failed to authenticate", err)
	}
	return nil
}

// truncationOr names the EOF-shaped failures as truncation and leaves every
// other failure — a wrong key, a flipped bit — reading as what it is.
func truncationOr(context string, err error) error {
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: %v", ErrTruncated, err)
	}
	return fmt.Errorf("%s: %w", context, err)
}

// stanza is one recipient stanza of an age header as the scan sees it.
type stanza struct {
	kind string
	args []string
	body strings.Builder
}

// checkStanzaExclusivity parses the recipient stanzas of an age header and
// applies two rules before any key is tried: scrypt is never beside another
// stanza, and a hybrid stanza is never beside any other type. The header is
// plain ASCII up to its MAC line: a stanza opens with "-> <type> <args>" and
// its base64 body lines follow, so the "-> " lines are the recipients.
func checkStanzaExclusivity(header []byte) error {
	var stanzas []*stanza
	s := bufio.NewScanner(bytes.NewReader(header))
	s.Buffer(nil, maxHeaderBytes+1)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "---") {
			break
		}
		if !strings.HasPrefix(line, "-> ") {
			// Body lines belong to the stanza above them; the version line
			// before the first stanza belongs to none.
			if n := len(stanzas); n > 0 {
				stanzas[n-1].body.WriteString(line)
			}
			continue
		}
		if len(stanzas) == maxRecipients {
			return ErrTooManyRecipients
		}
		fields := strings.Fields(line)
		st := &stanza{}
		if len(fields) >= 2 {
			st.kind, st.args = fields[1], fields[2:]
		}
		stanzas = append(stanzas, st)
	}
	if err := s.Err(); err != nil {
		return fmt.Errorf("backup: scan container header: %w", err)
	}
	if len(stanzas) == 0 {
		return errors.New("backup: container has no recipient stanzas")
	}
	var scrypt, hybrid int
	for _, st := range stanzas {
		switch st.kind {
		case "scrypt":
			scrypt++
		case hybridStanzaType:
			hybrid++
		}
	}
	if scrypt > 0 && len(stanzas) > 1 {
		return ErrMixedStanzas
	}
	if hybrid == 0 {
		return nil
	}
	if hybrid != len(stanzas) {
		return ErrMixedHybridStanzas
	}
	for _, st := range stanzas {
		if len(st.args) != 1 {
			return ErrMalformedHybridStanza
		}
		if enc, err := base64.RawStdEncoding.DecodeString(st.args[0]); err != nil || len(enc) != hybridEncapsulationBytes {
			return ErrMalformedHybridStanza
		}
		if body, err := base64.RawStdEncoding.DecodeString(st.body.String()); err != nil || len(body) != hybridWrappedKeyBytes {
			return ErrMalformedHybridStanza
		}
	}
	return nil
}
