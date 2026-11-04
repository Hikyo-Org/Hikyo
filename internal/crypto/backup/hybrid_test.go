package backup_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
)

func hybridIdentity(t *testing.T) (identity, recipient string) {
	t.Helper()
	id, rcp, err := backup.GenerateHybridIdentity()
	if err != nil {
		t.Fatalf("GenerateHybridIdentity: %v", err)
	}
	return id, rcp
}

func hybridRecipients(t *testing.T, n int) (identities, recipients []string) {
	t.Helper()
	for range n {
		id, rcp := hybridIdentity(t)
		identities = append(identities, id)
		recipients = append(recipients, rcp)
	}
	return identities, recipients
}

func TestHybridEncodingsAreNativeAndBounded(t *testing.T) {
	identity, recipient := hybridIdentity(t)
	if !strings.HasPrefix(recipient, "age1pq1") || len(recipient) != 1959 {
		t.Fatalf("hybrid recipient has prefix %q and %d characters, want age1pq1 and 1959", recipient[:7], len(recipient))
	}
	if !strings.HasPrefix(identity, "AGE-SECRET-KEY-PQ-1") || len(identity) != 77 {
		t.Fatalf("hybrid identity has %d characters, want 77", len(identity))
	}
	derived, err := backup.RecipientOf(identity)
	if err != nil || derived != recipient {
		t.Fatalf("RecipientOf(hybrid identity) = %q, %v; want the generated recipient", derived, err)
	}
	// Whitespace around a stored identity is not part of its canonical form.
	if derived, err = backup.RecipientOf("  " + identity + "\n"); err != nil || derived != recipient {
		t.Fatalf("RecipientOf must trim; got %v", err)
	}
}

func TestHybridRoundTrip(t *testing.T) {
	identity, recipient := hybridIdentity(t)
	want := payload()
	container := seal(t, backup.Options{Recipients: []string{recipient}}, want)
	if !bytes.Contains(container, []byte("-> mlkem768x25519 ")) {
		t.Fatal("hybrid container carries no mlkem768x25519 stanza")
	}
	if bytes.Contains(container, []byte("-> X25519")) || bytes.Contains(container, []byte("-> scrypt")) {
		t.Fatal("hybrid container carries a second stanza type")
	}
	got, err := extract(backup.Unlock{Identity: identity}, container)
	if err != nil {
		t.Fatalf("ExtractTo: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("round-tripped payload differs")
	}
}

func TestHybridMultipleRecipientsEitherOpens(t *testing.T) {
	ids, rcps := hybridRecipients(t, 3)
	want := []byte("three hybrid recipients")
	container := seal(t, backup.Options{Recipients: rcps}, want)
	for i, id := range ids {
		got, err := extract(backup.Unlock{Identity: id}, container)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("recipient %d: %v", i, err)
		}
	}
}

func TestHybridWrongKeyAndCrossAlgorithmRefused(t *testing.T) {
	_, hybrid := hybridIdentity(t)
	otherHybrid, _ := hybridIdentity(t)
	classicalID, classical := mustIdentity(t)
	hybridContainer := seal(t, backup.Options{Recipients: []string{hybrid}}, []byte("secret"))
	classicalContainer := seal(t, backup.Options{Recipients: []string{classical}}, []byte("secret"))

	if _, err := extract(backup.Unlock{Identity: otherHybrid}, hybridContainer); err == nil {
		t.Fatal("a foreign hybrid identity opened a hybrid container")
	}
	if _, err := extract(backup.Unlock{Identity: classicalID}, hybridContainer); err == nil {
		t.Fatal("a classical identity opened a hybrid container")
	}
	if _, err := extract(backup.Unlock{Identity: otherHybrid}, classicalContainer); err == nil {
		t.Fatal("a hybrid identity opened a classical container")
	}
}

func TestHybridTamperedAndTruncatedRefused(t *testing.T) {
	identity, recipient := hybridIdentity(t)
	container := seal(t, backup.Options{Recipients: []string{recipient}}, payload())

	body := bytes.Clone(container)
	body[len(body)-64] ^= 1
	if _, err := extract(backup.Unlock{Identity: identity}, body); err == nil {
		t.Fatal("a tampered hybrid payload decrypted")
	}
	// A flipped bit inside the encapsulation line changes the stanza the header
	// MAC covers, so it must fail even though ML-KEM rejects implicitly.
	header := bytes.Clone(container)
	header[bytes.Index(header, []byte("-> mlkem768x25519 "))+40] ^= 0x02
	if _, err := extract(backup.Unlock{Identity: identity}, header); err == nil {
		t.Fatal("a tampered hybrid header decrypted")
	}

	headerEnd := bytes.Index(container, []byte("---"))
	streamStart := headerEnd + bytes.IndexByte(container[headerEnd:], '\n') + 1
	const nonce, chunk = 16, 64*1024 + 16
	for name, cut := range map[string]int{
		"exact chunk boundary": streamStart + nonce + chunk,
		"no payload":           streamStart,
		"mid chunk":            len(container) - 1024,
		"mid header":           headerEnd / 2,
	} {
		out, err := extract(backup.Unlock{Identity: identity}, container[:cut])
		if err == nil {
			t.Fatalf("%s: truncated hybrid container extracted cleanly (%d bytes)", name, len(out))
		}
	}
	if _, err := extract(backup.Unlock{Identity: identity}, container[:streamStart+nonce+chunk]); !errors.Is(err, backup.ErrTruncated) {
		t.Fatalf("chunk-boundary truncation = %v, want ErrTruncated", err)
	}
}

func TestMixedRecipientsRefusedBeforeAnyOutput(t *testing.T) {
	_, hybrid := hybridIdentity(t)
	_, classical := mustIdentity(t)
	for name, rcps := range map[string][]string{
		"hybrid first":    {hybrid, classical},
		"classical first": {classical, hybrid},
	} {
		var out bytes.Buffer
		o := backup.Options{Recipients: rcps}
		if err := o.Validate(); !errors.Is(err, backup.ErrMixedRecipients) {
			t.Fatalf("%s: Validate = %v, want ErrMixedRecipients", name, err)
		}
		if _, err := backup.Encrypt(&out, o); !errors.Is(err, backup.ErrMixedRecipients) {
			t.Fatalf("%s: Encrypt = %v, want ErrMixedRecipients", name, err)
		}
		if out.Len() != 0 {
			t.Fatalf("%s: a refused mixed export wrote %d bytes", name, out.Len())
		}
	}
	if _, err := backup.Encrypt(io.Discard, backup.Options{Recipients: []string{hybrid}, Passphrase: "x"}); !errors.Is(err, backup.ErrRecipientExclusive) {
		t.Fatalf("hybrid plus passphrase = %v, want ErrRecipientExclusive", err)
	}
}

func TestHybridRecipientCountBound(t *testing.T) {
	_, rcps := hybridRecipients(t, backup.MaxHybridRecipients+1)

	var out bytes.Buffer
	if _, err := backup.Encrypt(&out, backup.Options{Recipients: rcps}); !errors.Is(err, backup.ErrTooManyHybridRecipients) || out.Len() != 0 {
		t.Fatalf("33 hybrid recipients = %v with %d bytes written, want ErrTooManyHybridRecipients and no output", err, out.Len())
	}

	// Exactly 32 is accepted. The measured header and the joined configuration
	// value must both fit their bounds with the count that is allowed.
	limit := rcps[:backup.MaxHybridRecipients]
	if err := (backup.Options{Recipients: limit}).Validate(); err != nil {
		t.Fatalf("32 hybrid recipients refused: %v", err)
	}
	joined := strings.Join(limit, ",")
	if len(joined) != 62719 || len(joined) > backup.MaxRecipientListBytes {
		t.Fatalf("32 joined recipients = %d bytes, want 62719 within %d", len(joined), backup.MaxRecipientListBytes)
	}
	container := seal(t, backup.Options{Recipients: limit}, []byte("x"))
	header := container[:bytes.Index(container, []byte("\n---"))+1]
	t.Logf("header at 32 hybrid recipients: %d bytes", len(header))
	if len(header) > 256<<10 {
		t.Fatalf("header at 32 hybrid recipients is %d bytes, over the 256 KiB reader bound", len(header))
	}
}

func TestRecipientListByteBoundNamesItself(t *testing.T) {
	// 32 valid recipients plus leading padding that a trim would hide must not
	// slip past the joined-value bound.
	_, rcps := hybridRecipients(t, 2)
	padded := []string{strings.Repeat(" ", 40000) + rcps[0], strings.Repeat(" ", 40000) + rcps[1]}
	if err := (backup.Options{Recipients: padded}).Validate(); !errors.Is(err, backup.ErrRecipientsTooLarge) {
		t.Fatalf("oversized list = %v, want ErrRecipientsTooLarge", err)
	}
}

func TestMalformedRecipientsAndIdentitiesRefused(t *testing.T) {
	identity, recipient := hybridIdentity(t)
	_, classical := mustIdentity(t)
	// A key ends in any bech32 character, so the replacement must differ from it.
	lastChar := "q"
	if strings.HasSuffix(recipient, lastChar) {
		lastChar = "p"
	}
	bad := map[string]string{
		"truncated":        recipient[:len(recipient)-1],
		"bad checksum":     recipient[:len(recipient)-1] + lastChar,
		"mixed case":       recipient[:20] + strings.ToUpper(recipient[20:]),
		"wrong hrp":        "age1pq2" + recipient[7:],
		"identity as key":  identity,
		"classical prefix": "age1pq1" + classical[4:],
		"oversized":        recipient + strings.Repeat("q", 4096),
		"unknown":          "age1unknown1qqqq",
		"empty":            "age1pq1",
	}
	for name, value := range bad {
		err := backup.Options{Recipients: []string{value}}.Validate()
		if err == nil {
			t.Fatalf("%s: malformed recipient accepted", name)
		}
		if strings.Contains(err.Error(), "AGE-SECRET-KEY") || strings.Contains(err.Error(), identity) {
			t.Fatalf("%s: the error echoed private material: %v", name, err)
		}
	}

	for name, value := range map[string]string{
		"truncated": identity[:len(identity)-1],
		"public":    recipient,
		"oversized": identity + strings.Repeat("Q", 4096),
		"garbage":   "AGE-SECRET-KEY-PQ-1notbech32",
	} {
		_, err := extract(backup.Unlock{Identity: value}, seal(t, backup.Options{Recipients: []string{recipient}}, []byte("x")))
		if err == nil {
			t.Fatalf("%s: malformed identity accepted", name)
		}
		if strings.Contains(err.Error(), "garbage") || strings.Contains(err.Error(), identity[:30]) {
			t.Fatalf("%s: the error echoed identity text: %v", name, err)
		}
		if _, err := backup.RecipientOf(value); err == nil {
			t.Fatalf("%s: RecipientOf accepted a malformed identity", name)
		}
	}
}

func TestDuplicateHybridRecipientsRefused(t *testing.T) {
	_, recipient := hybridIdentity(t)
	if err := (backup.Options{Recipients: []string{recipient, " " + recipient}}).Validate(); !errors.Is(err, backup.ErrDuplicateRecipient) {
		t.Fatalf("duplicate hybrid recipient = %v, want ErrDuplicateRecipient", err)
	}
}

// hybridStanza returns the stanza lines of a hybrid container for splicing.
func hybridStanza(t *testing.T, container []byte) string {
	return stanzaLines(t, container, "mlkem768x25519")
}

func TestReaderRefusesHybridBesideAnyOtherStanza(t *testing.T) {
	hybridID, hybridRcp := hybridIdentity(t)
	classicalID, classicalRcp := mustIdentity(t)
	hybrid := seal(t, backup.Options{Recipients: []string{hybridRcp}}, []byte("payload"))
	classical := seal(t, backup.Options{Recipients: []string{classicalRcp}}, []byte("payload"))
	passphrased := seal(t, backup.Options{Passphrase: "correct horse battery staple"}, []byte("payload"))

	plugin := "-> piv-p256 abc\n" + strings.Repeat("A", 43)
	ssh := "-> ssh-ed25519 abcd efgh\n" + strings.Repeat("A", 43)
	unknown := "-> future-kem 1 2\n" + strings.Repeat("A", 43)
	for name, other := range map[string]string{
		"x25519":  stanzaLines(t, classical, "X25519"),
		"scrypt":  stanzaLines(t, passphrased, "scrypt"),
		"plugin":  plugin,
		"ssh":     ssh,
		"unknown": unknown,
	} {
		mixed := spliceStanza(t, hybrid, other)
		for who, u := range map[string]backup.Unlock{
			"hybrid identity":    {Identity: hybridID},
			"classical identity": {Identity: classicalID},
			"passphrase":         {Passphrase: "correct horse battery staple"},
		} {
			_, err := extract(u, mixed)
			if !errors.Is(err, backup.ErrMixedStanzas) && !errors.Is(err, backup.ErrMixedHybridStanzas) {
				t.Fatalf("%s beside hybrid, opened with %s: %v, want a mixed-stanza refusal", name, who, err)
			}
		}
	}
}

func TestReaderRefusesMalformedHybridStanza(t *testing.T) {
	identity, recipient := hybridIdentity(t)
	container := seal(t, backup.Options{Recipients: []string{recipient}}, []byte("payload"))
	stanza := hybridStanza(t, container)
	lines := strings.Split(stanza, "\n")

	cases := map[string]string{
		"extra argument": lines[0] + " extra\n" + strings.Join(lines[1:], "\n"),
		"no argument":    "-> mlkem768x25519\n" + strings.Join(lines[1:], "\n"),
		"short body":     lines[0] + "\n" + strings.Repeat("A", 40),
		"long body":      lines[0] + "\n" + strings.Repeat("A", 64) + "\n" + strings.Repeat("A", 22),
		"short enc":      "-> mlkem768x25519 " + base64.RawStdEncoding.EncodeToString(make([]byte, 32)) + "\n" + strings.Join(lines[1:], "\n"),
	}
	for name, replacement := range cases {
		mutated := bytes.Replace(container, []byte(stanza), []byte(replacement), 1)
		if bytes.Equal(mutated, container) {
			t.Fatalf("%s: mutation did not apply", name)
		}
		if _, err := extract(backup.Unlock{Identity: identity}, mutated); !errors.Is(err, backup.ErrMalformedHybridStanza) {
			t.Fatalf("%s: = %v, want ErrMalformedHybridStanza", name, err)
		}
	}
}

// age's own header parser refuses a body that is not base64, before the scan.
func TestReaderRefusesNonBase64HybridBody(t *testing.T) {
	identity, recipient := hybridIdentity(t)
	container := seal(t, backup.Options{Recipients: []string{recipient}}, []byte("payload"))
	stanza := hybridStanza(t, container)
	lines := strings.Split(stanza, "\n")
	mutated := bytes.Replace(container, []byte(stanza), []byte(lines[0]+"\n"+strings.Repeat("!", 43)), 1)
	if _, err := extract(backup.Unlock{Identity: identity}, mutated); err == nil {
		t.Fatal("a hybrid stanza with a non-base64 body was accepted")
	}
}

func TestFingerprintNamespaces(t *testing.T) {
	_, hybrid := hybridIdentity(t)
	_, classical := mustIdentity(t)

	hybridFP, err := backup.Options{Recipients: []string{hybrid}}.UpgradeRecipientFingerprints()
	if err != nil || len(hybridFP) != 1 {
		t.Fatalf("hybrid fingerprints: %v", err)
	}
	digest := sha256.Sum256([]byte(hybrid))
	if want := "age-hybrid-mlkem768x25519-sha256:" + hex.EncodeToString(digest[:]); hybridFP[0] != want {
		t.Fatalf("hybrid fingerprint = %s, want %s", hybridFP[0], want)
	}

	// The classical namespace keeps its exact historical meaning.
	classicalFP, err := backup.Options{Recipients: []string{classical}}.UpgradeRecipientFingerprints()
	if err != nil || len(classicalFP) != 1 {
		t.Fatalf("classical fingerprints: %v", err)
	}
	digest = sha256.Sum256([]byte(classical))
	if want := "age-x25519-sha256:" + hex.EncodeToString(digest[:]); classicalFP[0] != want {
		t.Fatalf("classical fingerprint = %s, want %s", classicalFP[0], want)
	}

	// The same fingerprint function names the held identity's recipient.
	id, rcp := hybridIdentity(t)
	held, err := backup.Unlock{Identity: id}.UpgradeRecipientFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	listed, _ := backup.Options{Recipients: []string{rcp}}.UpgradeRecipientFingerprints()
	if held != listed[0] || !strings.HasPrefix(held, "age-hybrid-mlkem768x25519-sha256:") {
		t.Fatalf("held fingerprint %s != listed %s", held, listed[0])
	}

	if _, err := (backup.Options{Recipients: []string{hybrid, classical}}).UpgradeRecipientFingerprints(); !errors.Is(err, backup.ErrMixedRecipients) {
		t.Fatalf("mixed upgrade inventory = %v, want ErrMixedRecipients", err)
	}
	_, many := hybridRecipients(t, backup.MaxHybridRecipients+1)
	if _, err := (backup.Options{Recipients: many}).UpgradeRecipientFingerprints(); !errors.Is(err, backup.ErrTooManyHybridRecipients) {
		t.Fatalf("33 hybrid upgrade recipients = %v, want ErrTooManyHybridRecipients", err)
	}

	alg, sum, ok := backup.ParseFingerprint(hybridFP[0])
	if !ok || alg != backup.AlgorithmHybrid || len(sum) != 64 {
		t.Fatalf("ParseFingerprint(hybrid) = %v %q %v", alg, sum, ok)
	}
	alg, _, ok = backup.ParseFingerprint(classicalFP[0])
	if !ok || alg != backup.AlgorithmX25519 {
		t.Fatalf("ParseFingerprint(classical) = %v %v", alg, ok)
	}
	for _, bad := range []string{"", "age-x25519-sha256:", "age-hybrid-mlkem768x25519-sha256:zz", "age-pq-sha256:" + sum, "age-x25519-sha256:" + strings.ToUpper(sum)} {
		if _, _, ok := backup.ParseFingerprint(bad); ok {
			t.Fatalf("ParseFingerprint accepted %q", bad)
		}
	}
}

// Frozen archives were written by the code before hybrid support existed. They
// stay readable, byte for byte, by the code after it.
func TestFrozenLegacyArchivesStayReadable(t *testing.T) {
	read := func(name string) []byte {
		t.Helper()
		raw, err := os.ReadFile("testdata/legacy/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	sums := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(read("SHA256SUMS"))), "\n") {
		fields := strings.SplitN(line, "  ", 2)
		sums[fields[1]] = fields[0]
	}
	plaintext := payload()
	if digest := sha256.Sum256(plaintext); hex.EncodeToString(digest[:]) != sums["plaintext (payload())"] {
		t.Fatal("the fixture plaintext generator drifted")
	}
	for name, unlock := range map[string]backup.Unlock{
		"x25519.age":     {Identity: strings.TrimSpace(string(read("x25519.identity")))},
		"passphrase.age": {Passphrase: strings.TrimSpace(string(read("passphrase.txt")))},
	} {
		archive := read(name)
		if digest := sha256.Sum256(archive); hex.EncodeToString(digest[:]) != sums[name] {
			t.Fatalf("%s no longer matches its recorded digest: the fixture is frozen", name)
		}
		got, err := extract(unlock, archive)
		if err != nil || !bytes.Equal(got, plaintext) {
			t.Fatalf("%s: frozen legacy archive unreadable: %v", name, err)
		}
	}
}

// One predicate decides which refusals may be shown by name; a malformed key
// is deliberately not among them.
func TestPolicyErrorPredicateAndHybridSelection(t *testing.T) {
	_, hybrid := hybridIdentity(t)
	_, classical := mustIdentity(t)
	for name, tc := range map[string]struct {
		options backup.Options
		policy  bool
	}{
		"mixed":     {backup.Options{Recipients: []string{hybrid, classical}}, true},
		"duplicate": {backup.Options{Recipients: []string{hybrid, hybrid}}, true},
		"malformed": {backup.Options{Recipients: []string{hybrid[:50]}}, false},
	} {
		err := tc.options.Validate()
		if err == nil || backup.IsPolicyError(err) != tc.policy {
			t.Fatalf("%s: Validate = %v, IsPolicyError = %t, want %t", name, err, backup.IsPolicyError(err), tc.policy)
		}
	}
	if backup.IsPolicyError(nil) || backup.IsPolicyError(errors.New("other")) {
		t.Fatal("unrelated errors classified as policy refusals")
	}
	if !backup.AnyHybridRecipient([]string{classical, " " + hybrid}) || backup.AnyHybridRecipient([]string{classical}) || backup.AnyHybridRecipient(nil) {
		t.Fatal("hybrid selection misclassified a recipient list")
	}
}
