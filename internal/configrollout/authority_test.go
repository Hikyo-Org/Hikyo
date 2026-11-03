package configrollout

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// v1Golden is the SHA-256 of the signed command serialized by the code at
// e5539c280 (before this change) for the fixed key and command below. The
// compatibility profile must stay byte-identical to it.
const v1Golden = "0901486c9872dedfae0f5e83f6f49bcbefb58f048899f521f78596ea17d189e1"

func goldenCommand() Command {
	issued := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	return Command{EnrollmentID: "enrollment-1", Sequence: 7, Action: ActionSubmit, Intent: Intent{JobID: "job-1", OwnerInstanceID: "owner-1", Incarnation: "inc-1", SnapshotID: "snap-1", Revision: 3, CatalogueVersion: 2, ExpectedGeneration: 4, Generation: 5}, PlanDigest: strings.Repeat("0", 63) + "1", IssuedAt: issued, ExpiresAt: issued.Add(5 * time.Minute)}
}

func TestCompatibilityProfileKeepsV1BytesAndSignerPath(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	private := ed25519.NewKeyFromSeed(seed)
	e := Enrollment{ID: "enrollment-1"}
	legacy, err := SignCommand(t.Context(), private, goldenCommand())
	if err != nil {
		t.Fatal(err)
	}
	viaSet, err := SignCommandFor(t.Context(), e, Signers{Ed25519: private}, goldenCommand())
	if err != nil {
		t.Fatal(err)
	}
	for name, signed := range map[string]SignedCommand{"SignCommand": legacy, "SignCommandFor": viaSet} {
		raw, err := json.Marshal(signed)
		sum := sha256.Sum256(raw)
		if err != nil || hex.EncodeToString(sum[:]) != v1Golden {
			t.Fatalf("%s changed the v1 bytes: %s", name, raw)
		}
		if bytes.Contains(raw, []byte("authority")) || bytes.Contains(raw, []byte("signatures")) {
			t.Fatalf("%s leaked strict fields into v1: %s", name, raw)
		}
	}
	input, ok := commandInputV1(goldenCommand())
	if !ok || !ed25519.Verify(private.Public().(ed25519.PublicKey), input, legacy.Signature) || !bytes.HasPrefix(input, []byte("hikyo.configuration-rollout.command.v1\x00")) {
		t.Fatal("v1 signed bytes are not the documented domain plus command JSON")
	}
}

func TestKeyIDsAreAlgorithmBearingAndDomainSeparated(t *testing.T) {
	raw := bytes.Repeat([]byte{7}, 32)
	a, b := KeyID(AlgEd25519, raw), KeyID(AlgMLDSA65, raw)
	if a == b || !strings.HasPrefix(a, "ed25519-sha256:") || !strings.HasPrefix(b, "mldsa65-sha256:") || !validKeyID(AlgEd25519, a) || validKeyID(AlgMLDSA65, a) {
		t.Fatal(a, b)
	}
	if a[len("ed25519-sha256:"):] == b[len("mldsa65-sha256:"):] {
		t.Fatal("algorithm is not inside the hashed bytes")
	}
	plain := sha256.Sum256(raw)
	if hex.EncodeToString(plain[:]) == a[len("ed25519-sha256:"):] {
		t.Fatal("key id lacks domain separation")
	}
}

// authorityFixture is a real producer, mailbox and controller on the fake
// Kubernetes API for one trust profile.
type authorityFixture struct {
	fixture
	e       Enrollment
	c       *Controller
	m       *Mailbox
	signers Signers
	keys    PublicKeys
}

func newKeyMaterial(t testing.TB) (Signers, PublicKeys) {
	t.Helper()
	_, edPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pq, err := mldsa.GenerateKey(mldsa.MLDSA65())
	if err != nil {
		t.Fatal(err)
	}
	signers := Signers{Ed25519: edPrivate, MLDSA65: pq}
	return signers, signers.Public()
}

func authorityFor(generation uint64, required []string, keys PublicKeys) *Authority {
	ids := keys.KeyIDs()
	a := &Authority{Version: 1, Generation: generation, Required: slices.Clone(required)}
	for _, algorithm := range required {
		a.KeyIDs = append(a.KeyIDs, ids[algorithm])
	}
	return a
}

func narrowKeys(keys PublicKeys, required []string) PublicKeys {
	out := PublicKeys{}
	if slices.Contains(required, AlgEd25519) {
		out.Ed25519 = keys.Ed25519
	}
	if slices.Contains(required, AlgMLDSA65) {
		out.MLDSA65 = keys.MLDSA65
	}
	return out
}

func narrowSigners(signers Signers, required []string) Signers {
	out := Signers{}
	if slices.Contains(required, AlgEd25519) {
		out.Ed25519 = signers.Ed25519
	}
	if slices.Contains(required, AlgMLDSA65) {
		out.MLDSA65 = signers.MLDSA65
	}
	return out
}

func newAuthorityFixture(t *testing.T, generation uint64, required []string) authorityFixture {
	t.Helper()
	f, e, _, _, _ := controllerFixture(t)
	signers, keys := newKeyMaterial(t)
	e.Authority = authorityFor(generation, required, keys)
	return rebuild(t, f, e, narrowSigners(signers, required), narrowKeys(keys, required))
}

func rebuild(t *testing.T, f fixture, e Enrollment, signers Signers, keys PublicKeys) authorityFixture {
	t.Helper()
	c, err := NewControllerKeys(f.client, e, keys)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewMailbox(f.client, e)
	if err != nil {
		t.Fatal(err)
	}
	return authorityFixture{fixture: f, e: e, c: c, m: m, signers: signers, keys: keys}
}

func (a authorityFixture) sign(t *testing.T, command Command) SignedCommand {
	t.Helper()
	command.EnrollmentID = a.e.ID
	command.IssuedAt = time.Now().UTC().Add(-time.Second)
	command.ExpiresAt = command.IssuedAt.Add(5 * time.Minute)
	signed, err := SignCommandFor(t.Context(), a.e, a.signers, command)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func (a authorityFixture) prepare(t *testing.T, sequence uint64) SignedCommand {
	return a.sign(t, Command{Sequence: sequence, Action: ActionPrepare, Intent: a.intent, Changes: a.changes()})
}

// plant writes a command into the mailbox as any holder of command-Secret write
// could, bypassing the producer's own structural checks.
func (a authorityFixture) plant(t *testing.T, signed SignedCommand) {
	t.Helper()
	s := a.secret(t, "command")
	putRecord(s, commandKey, signed)
	if _, err := a.client.CoreV1().Secrets("hikyo").Update(t.Context(), s, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func mutations(f fixture) []string {
	var out []string
	for _, action := range f.client.Actions() {
		if action.GetVerb() != "get" && action.GetVerb() != "list" && action.GetVerb() != "watch" {
			out = append(out, action.GetVerb()+" "+action.GetResource().Resource)
		}
	}
	return out
}

// refused plants the command, runs one controller tick and proves no write of
// any kind reached the Kubernetes API.
func (a authorityFixture) refused(t *testing.T, signed SignedCommand, want error) {
	t.Helper()
	a.plant(t, signed)
	a.client.ClearActions()
	if err := a.c.reconcile(t.Context()); !errors.Is(err, want) {
		t.Fatalf("want %v, got %v", want, err)
	}
	if got := mutations(a.fixture); len(got) != 0 {
		t.Fatalf("refused input caused mutations: %v", got)
	}
}

func (a authorityFixture) accepted(t *testing.T, signed SignedCommand) Response {
	t.Helper()
	a.plant(t, signed)
	if err := a.c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	response, err := a.m.Response(t.Context(), signed)
	if err != nil || response.Outcome != "complete" {
		t.Fatal(response, err)
	}
	return response
}

var profiles = map[string][]string{"dual": requiredDual, "pq-only": requiredPQOnly}

func TestStrictProfileSignDeliverVerifyExecuteAndRestart(t *testing.T) {
	for name, required := range profiles {
		t.Run(name, func(t *testing.T) {
			a := newAuthorityFixture(t, 1, required)
			prepare := a.prepare(t, 1)
			if len(prepare.Signature) != 0 || len(prepare.Signatures) != len(required) {
				t.Fatalf("envelope %+v", prepare)
			}
			if err := a.m.Send(t.Context(), prepare); err != nil {
				t.Fatal(err)
			}
			if err := a.c.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			response, err := a.m.Response(t.Context(), prepare)
			if err != nil || !validDigest(response.PlanDigest) {
				t.Fatal(response, err)
			}
			if digest(a.deployment(t).Spec) != digest(a.before.Spec) {
				t.Fatal("planning mutated the deployment")
			}
			submit := a.sign(t, Command{Sequence: 2, Action: ActionSubmit, Intent: a.intent, PlanDigest: response.PlanDigest})
			if err := a.m.Send(t.Context(), submit); err != nil {
				t.Fatal(err)
			}
			// Restart: a new process holds the same enrollment and keys.
			restarted, err := NewControllerKeys(a.client, a.e, a.keys)
			if err != nil {
				t.Fatal(err)
			}
			if err := restarted.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			done, err := a.m.Response(t.Context(), submit)
			if err != nil || done.Receipt == nil || done.Receipt.Phase != RolloutRequested {
				t.Fatal(done, err)
			}
			if digest(a.deployment(t).Spec) == digest(a.before.Spec) {
				t.Fatal("authorized submit did not change the deployment")
			}
			var journal controllerJournal
			if decode(a.secret(t, "journal").Data[journalKey], &journal) != nil || journal.Accepted.Command.Authority == nil {
				t.Fatal("journal does not retain the strict command")
			}
			var floor trustRecord
			if decode(a.secret(t, "journal").Data[trustKey], &floor) != nil || floor.Generation != 1 {
				t.Fatal("trust floor not persisted with the acceptance")
			}
		})
	}
}

func TestStrictProfileRefusesWithZeroMutation(t *testing.T) {
	for name, required := range profiles {
		for _, mode := range []string{
			"v1 signature only", "stripped ML-DSA", "stripped Ed25519", "wrong algorithm set", "duplicate signature", "reordered signatures",
			"short ML-DSA signature", "tampered ML-DSA", "tampered Ed25519", "tampered payload", "wrong key id", "other key pair",
			"wrong generation", "wrong target digest", "no binding", "wrong enrollment", "wrong action sequence zero",
			"expired", "future", "legacy field beside strict", "unknown algorithm", "wrong instance", "wrong action",
		} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				a := newAuthorityFixture(t, 4, required)
				signed := a.prepare(t, 1)
				switch mode {
				case "v1 signature only":
					edKey := a.signers.Ed25519
					if edKey == nil {
						_, edKey, _ = ed25519.GenerateKey(rand.Reader)
					}
					legacy := signed.Command
					legacy.Authority = nil
					v1, _ := SignCommand(t.Context(), edKey, legacy)
					signed = v1
				case "stripped ML-DSA":
					signed.Signatures = slices.DeleteFunc(signed.Signatures, func(s CommandSignature) bool { return s.Algorithm == AlgMLDSA65 })
				case "stripped Ed25519":
					if len(required) == 1 {
						t.Skip("not required")
					}
					signed.Signatures = slices.DeleteFunc(signed.Signatures, func(s CommandSignature) bool { return s.Algorithm == AlgEd25519 })
				case "wrong algorithm set":
					if len(required) == 2 {
						signed.Signatures = signed.Signatures[1:]
					} else {
						_, edKey, _ := ed25519.GenerateKey(rand.Reader)
						input, _ := commandInputV2(signed.Command)
						sig, _ := edKey.Sign(rand.Reader, input, crypto.Hash(0))
						signed.Signatures = []CommandSignature{{Algorithm: AlgEd25519, KeyID: KeyID(AlgEd25519, edKey.Public().(ed25519.PublicKey)), Signature: sig}}
					}
				case "duplicate signature":
					signed.Signatures = append(signed.Signatures, signed.Signatures[len(signed.Signatures)-1])
				case "reordered signatures":
					if len(required) == 1 {
						t.Skip("single signature")
					}
					signed.Signatures[0], signed.Signatures[1] = signed.Signatures[1], signed.Signatures[0]
				case "short ML-DSA signature":
					for i := range signed.Signatures {
						if signed.Signatures[i].Algorithm == AlgMLDSA65 {
							signed.Signatures[i].Signature = signed.Signatures[i].Signature[:3308]
						}
					}
				case "tampered ML-DSA", "tampered Ed25519":
					algorithm := AlgMLDSA65
					if mode == "tampered Ed25519" {
						algorithm = AlgEd25519
					}
					found := false
					for i := range signed.Signatures {
						if signed.Signatures[i].Algorithm == algorithm {
							signed.Signatures[i].Signature[0] ^= 1
							found = true
						}
					}
					if !found {
						t.Skip("algorithm not required")
					}
				case "tampered payload":
					signed.Command.Changes[0].Value = "999"
				case "wrong key id":
					signed.Signatures[len(signed.Signatures)-1].KeyID = KeyID(AlgMLDSA65, []byte("other"))
				case "other key pair":
					other, _ := newKeyMaterial(t)
					a2 := a
					a2.e.Authority = authorityFor(4, required, other.Public())
					a2.signers = narrowSigners(other, required)
					signed = a2.prepare(t, 1)
				case "wrong generation":
					other := a
					other.e.Authority = authorityFor(3, required, a.keys)
					signed = other.prepare(t, 1)
				case "wrong target digest":
					signed.Command.Authority.TargetDigest = strings.Repeat("a", 64)
				case "no binding":
					signed.Command.Authority = nil
				case "wrong enrollment":
					signed.Command.EnrollmentID = "other"
				case "wrong action sequence zero":
					signed.Command.Sequence = 0
				case "expired":
					signed.Command.IssuedAt = time.Now().Add(-time.Hour)
					signed.Command.ExpiresAt = signed.Command.IssuedAt.Add(time.Minute)
					signed, _ = SignCommandFor(t.Context(), a.e, a.signers, signed.Command)
				case "future":
					signed.Command.IssuedAt = time.Now().Add(time.Hour)
					signed.Command.ExpiresAt = signed.Command.IssuedAt.Add(time.Minute)
					signed, _ = SignCommandFor(t.Context(), a.e, a.signers, signed.Command)
				case "legacy field beside strict":
					signed.Signature = bytes.Repeat([]byte{1}, ed25519.SignatureSize)
				case "unknown algorithm":
					signed.Signatures[0].Algorithm = "mldsa44"
				case "wrong instance":
					signed.Command.Intent.OwnerInstanceID = "other-instance"
				case "wrong action":
					// A shape-valid action swap: only the signatures notice.
					signed = a.sign(t, Command{Sequence: 1, Action: ActionSubmit, Intent: a.intent, PlanDigest: strings.Repeat("a", 64)})
					signed.Command.Action = ActionRestore
				}
				a.refused(t, signed, ErrInvalid)
				// Shape errors are also refused by the producer's mailbox, which
				// must not write the command Secret at all. Cryptographic errors
				// are the controller's to refuse; the mailbox checks shape only.
				if structural[mode] {
					a.client.ClearActions()
					if err := a.m.Send(t.Context(), signed); !errors.Is(err, ErrInvalid) {
						t.Fatalf("mailbox accepted %s: %v", mode, err)
					}
					if got := mutations(a.fixture); len(got) != 0 {
						t.Fatalf("mailbox wrote refused input: %v", got)
					}
				}
			})
		}
	}
}

// structural modes fail the key-free shape check shared by producer and verifier.
var structural = map[string]bool{
	"v1 signature only": true, "stripped ML-DSA": true, "stripped Ed25519": true, "wrong algorithm set": true, "duplicate signature": true,
	"reordered signatures": true, "short ML-DSA signature": true, "wrong key id": true, "other key pair": true, "wrong generation": true,
	"wrong target digest": true, "no binding": true, "wrong enrollment": true, "wrong action sequence zero": true,
	"legacy field beside strict": true, "unknown algorithm": true, "wrong instance": true,
}

// enrollmentV0 is Enrollment as it was before the Authority field.
type enrollmentV0 struct {
	ID                string `json:"id"`
	OwnerInstanceID   string `json:"owner_instance_id"`
	Incarnation       string `json:"incarnation"`
	Target            Target `json:"target"`
	CommandSecret     string `json:"command_secret"`
	CommandSecretUID  string `json:"command_secret_uid"`
	ResponseSecret    string `json:"response_secret"`
	ResponseSecretUID string `json:"response_secret_uid"`
	JournalSecret     string `json:"journal_secret"`
	JournalSecretUID  string `json:"journal_secret_uid"`
	LeaseName         string `json:"lease_name"`
	LeaseUID          string `json:"lease_uid"`
	ExecutorPod       string `json:"executor_pod"`
}

// signedCommandV0 is SignedCommand as it was before the strict envelope.
type signedCommandV0 struct {
	Command   json.RawMessage `json:"command"`
	Signature []byte          `json:"signature"`
}

func strictDecode(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}

func TestOldAndNewControllerAndSignerCombinations(t *testing.T) {
	a := newAuthorityFixture(t, 1, requiredDual)
	strict := a.prepare(t, 1)

	// New controller, old signer: a v1 command is refused under strict.
	legacy := signedFixture(t, a.signers.Ed25519.(ed25519.PrivateKey), a.e, Command{Sequence: 1, Action: ActionPrepare, Intent: a.intent, Changes: a.changes()})
	a.refused(t, legacy, ErrInvalid)

	// Old controller, new signer: the previous decoders disallow unknown fields,
	// so they refuse the strict envelope and the strict enrollment before any
	// verification (reader before writer, with a visible refusal).
	if strictDecode(mustJSON(strict), &signedCommandV0{}) == nil {
		t.Fatal("a pre-strict command decoder accepted a strict command")
	}
	if strictDecode(mustJSON(a.e), &enrollmentV0{}) == nil {
		t.Fatal("a pre-strict enrollment decoder accepted a strict enrollment")
	}
	// The previous decoders accept what the compatibility profile still emits.
	f, e, c, _, private := controllerFixture(t)
	compat := signedFixture(t, private, e, Command{Sequence: 1, Action: ActionPrepare, Intent: f.intent, Changes: f.changes()})
	var raw signedCommandV0
	if err := strictDecode(mustJSON(compat), &raw); err != nil {
		t.Fatal("compatibility command no longer decodes as v1:", err)
	}
	if err := strictDecode(mustJSON(e), &enrollmentV0{}); err != nil {
		t.Fatal("compatibility enrollment no longer decodes as v1:", err)
	}

	// Compatibility controller of this build refuses a strict command.
	b := authorityFixture{fixture: f, e: e, c: c, signers: a.signers}
	b.refused(t, strict, ErrInvalid)

	// Compatibility controller and signer still complete the whole path.
	good := authorityFixture{fixture: f, e: e, c: c, signers: Signers{Ed25519: private}}
	good.m, _ = NewMailbox(f.client, e)
	if got := good.accepted(t, good.prepare(t, 1)); !validDigest(got.PlanDigest) {
		t.Fatal(got)
	}
}

func TestEnrollmentAuthorityIsClosedAndParsedStrictly(t *testing.T) {
	_, keys := newKeyMaterial(t)
	f, e, _, _, _ := controllerFixture(t)
	_ = f
	good := authorityFor(1, requiredDual, keys)
	mutate := func(edit func(*Authority)) Enrollment {
		a := cloneAuthority(*good)
		edit(&a)
		out := e
		out.Authority = &a
		return out
	}
	for name, enrollment := range map[string]Enrollment{
		"version":         mutate(func(a *Authority) { a.Version = 2 }),
		"generation zero": mutate(func(a *Authority) { a.Generation = 0 }),
		"ed25519 only":    mutate(func(a *Authority) { a.Required, a.KeyIDs = a.Required[:1], a.KeyIDs[:1] }),
		"unknown":         mutate(func(a *Authority) { a.Required = []string{AlgMLDSA65, "slhdsa"} }),
		"order": mutate(func(a *Authority) {
			a.Required = []string{AlgMLDSA65, AlgEd25519}
			a.KeyIDs = []string{a.KeyIDs[1], a.KeyIDs[0]}
		}),
		"id count":     mutate(func(a *Authority) { a.KeyIDs = a.KeyIDs[:1] }),
		"id algorithm": mutate(func(a *Authority) { a.KeyIDs = []string{a.KeyIDs[1], a.KeyIDs[1]} }),
		"id uppercase": mutate(func(a *Authority) { a.KeyIDs[0] = strings.ToUpper(a.KeyIDs[0]) }),
		"id short":     mutate(func(a *Authority) { a.KeyIDs[0] = a.KeyIDs[0][:len(a.KeyIDs[0])-2] }),
	} {
		if validEnrollment(enrollment) {
			t.Errorf("%s: invalid authority accepted", name)
		}
		if _, err := ParseEnrollment(mustJSON(enrollment)); err == nil {
			t.Errorf("%s: parser accepted invalid authority", name)
		}
	}
	withUnknown := strings.Replace(string(mustJSON(mutate(func(*Authority) {}))), `"version":1`, `"version":1,"optional":true`, 1)
	if _, err := ParseEnrollment([]byte(withUnknown)); err == nil {
		t.Error("unknown authority field accepted")
	}
	if _, err := ParseEnrollment(mustJSON(mutate(func(*Authority) {}))); err != nil {
		t.Error("valid strict enrollment refused:", err)
	}
}

func pemOf(t *testing.T, blockType string, der []byte) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
}

func TestKeyFilesAreClosedSets(t *testing.T) {
	signers, keys := newKeyMaterial(t)
	edPub, _ := x509.MarshalPKIXPublicKey(keys.Ed25519)
	pqPub, _ := x509.MarshalPKIXPublicKey(keys.MLDSA65)
	edPriv, _ := x509.MarshalPKCS8PrivateKey(signers.Ed25519)
	pqPriv, _ := x509.MarshalPKCS8PrivateKey(signers.MLDSA65)
	pq44, _ := mldsa.GenerateKey(mldsa.MLDSA44())
	pq87, _ := mldsa.GenerateKey(mldsa.MLDSA87())
	pq44Pub, _ := x509.MarshalPKIXPublicKey(pq44.PublicKey())
	pq44Priv, _ := x509.MarshalPKCS8PrivateKey(pq44)
	pq87Pub, _ := x509.MarshalPKIXPublicKey(pq87.PublicKey())
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, _ := x509.MarshalPKIXPublicKey(other.Public())
	otherPriv, _ := x509.MarshalPKCS8PrivateKey(other)
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

	for name, tc := range map[string]struct {
		raw []byte
		ok  bool
	}{
		"ed25519":             {pemOf(t, "PUBLIC KEY", edPub), true},
		"ml-dsa-65":           {pemOf(t, "PUBLIC KEY", pqPub), true},
		"dual":                {cat(pemOf(t, "PUBLIC KEY", edPub), pemOf(t, "PUBLIC KEY", pqPub)), true},
		"dual reversed":       {cat(pemOf(t, "PUBLIC KEY", pqPub), pemOf(t, "PUBLIC KEY", edPub)), true},
		"ml-dsa-44":           {pemOf(t, "PUBLIC KEY", pq44Pub), false},
		"ml-dsa-87":           {pemOf(t, "PUBLIC KEY", pq87Pub), false},
		"duplicate algorithm": {cat(pemOf(t, "PUBLIC KEY", edPub), pemOf(t, "PUBLIC KEY", otherPub)), false},
		"three blocks":        {cat(pemOf(t, "PUBLIC KEY", edPub), pemOf(t, "PUBLIC KEY", pqPub), pemOf(t, "PUBLIC KEY", pqPub)), false},
		"trailing junk":       {cat(pemOf(t, "PUBLIC KEY", edPub), []byte("junk")), false},
		"preamble junk":       {cat([]byte("junk\n"), pemOf(t, "PUBLIC KEY", edPub)), false},
		"empty":               {nil, false},
		"private as public":   {pemOf(t, "PRIVATE KEY", edPriv), false},
		"wrong block type":    {pemOf(t, "CERTIFICATE", edPub), false},
		"header":              {pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Headers: map[string]string{"a": "b"}, Bytes: edPub}), false},
		"not a key":           {pemOf(t, "PUBLIC KEY", []byte("nope")), false},
	} {
		_, err := ParsePublicKeys(tc.raw)
		if (err == nil) != tc.ok {
			t.Errorf("public %s: err=%v want ok=%v", name, err, tc.ok)
		}
	}
	for name, tc := range map[string]struct {
		raw []byte
		ok  bool
	}{
		"ed25519":             {pemOf(t, "PRIVATE KEY", edPriv), true},
		"ml-dsa-65 seed only": {pemOf(t, "PRIVATE KEY", pqPriv), true},
		"dual":                {cat(pemOf(t, "PRIVATE KEY", edPriv), pemOf(t, "PRIVATE KEY", pqPriv)), true},
		"ml-dsa-44":           {pemOf(t, "PRIVATE KEY", pq44Priv), false},
		"duplicate":           {cat(pemOf(t, "PRIVATE KEY", edPriv), pemOf(t, "PRIVATE KEY", otherPriv)), false},
		"public as private":   {pemOf(t, "PUBLIC KEY", edPub), false},
		"trailing junk":       {cat(pemOf(t, "PRIVATE KEY", edPriv), []byte("x")), false},
		"empty":               {nil, false},
	} {
		parsed, err := ParseSigners(tc.raw)
		if (err == nil) != tc.ok {
			t.Errorf("private %s: err=%v want ok=%v", name, err, tc.ok)
		}
		if err == nil && name == "dual" && (parsed.Ed25519 == nil || parsed.MLDSA65 == nil) {
			t.Error("dual signer file lost a key")
		}
	}
}

func TestExpandedMLDSAPrivateKeyFormsAreRefused(t *testing.T) {
	pq, err := mldsa.GenerateKey(mldsa.MLDSA65())
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(pq)
	if err != nil {
		t.Fatal(err)
	}
	// The inner OCTET STRING is the seed-only choice 0x80 + length. Flip the
	// choice tag to the expanded-key tag: Go must refuse and so must we.
	i := bytes.Index(der, []byte{0x80, 32})
	if i < 0 {
		t.Fatal("seed tag not found")
	}
	bad := slices.Clone(der)
	bad[i] = 0x04
	if _, err := ParseSigners(pemOf(t, "PRIVATE KEY", bad)); err == nil {
		t.Fatal("non seed-only ML-DSA private key accepted")
	}
}

func TestCheckKeysPinsExactlyTheEnrolledKeys(t *testing.T) {
	_, keys := newKeyMaterial(t)
	_, other := newKeyMaterial(t)
	f, e, _, _, _ := controllerFixture(t)
	_ = f
	e.Authority = authorityFor(1, requiredDual, keys)
	if err := CheckKeys(e, keys); err != nil {
		t.Fatal(err)
	}
	for name, held := range map[string]PublicKeys{
		"missing ML-DSA":    {Ed25519: keys.Ed25519},
		"missing Ed25519":   {MLDSA65: keys.MLDSA65},
		"swapped ML-DSA":    {Ed25519: keys.Ed25519, MLDSA65: other.MLDSA65},
		"swapped Ed25519":   {Ed25519: other.Ed25519, MLDSA65: keys.MLDSA65},
		"none":              {},
		"truncated Ed25519": {Ed25519: keys.Ed25519[:31], MLDSA65: keys.MLDSA65},
	} {
		if err := CheckKeys(e, held); !errors.Is(err, ErrTrust) {
			t.Errorf("%s: %v", name, err)
		}
	}
	e.Authority = authorityFor(2, requiredPQOnly, keys)
	if err := CheckKeys(e, PublicKeys{MLDSA65: keys.MLDSA65}); err != nil {
		t.Fatal(err)
	}
	if err := CheckKeys(e, keys); !errors.Is(err, ErrTrust) {
		t.Fatal("PQ-only verifier kept an unpinned Ed25519 key:", err)
	}
	e.Authority = nil
	if err := CheckKeys(e, PublicKeys{Ed25519: keys.Ed25519}); err != nil {
		t.Fatal(err)
	}
	if err := CheckKeys(e, keys); err == nil {
		t.Fatal("compatibility verifier accepted an ML-DSA key it never enrolled")
	}
	signers, _ := newKeyMaterial(t)
	_, strictKeys := newKeyMaterial(t)
	e.Authority = authorityFor(1, requiredDual, strictKeys)
	if _, err := SignCommandFor(t.Context(), e, signers, Command{}); err == nil {
		t.Fatal("producer signed with keys that are not the enrolled ones")
	}
	e.Authority = nil
	if _, err := SignCommandFor(t.Context(), e, signers, Command{}); err == nil {
		t.Fatal("compatibility producer accepted an ML-DSA signer")
	}
}

func TestSignaturesAreSuiteBoundAcrossProfiles(t *testing.T) {
	// A signature made under the {ed25519, mldsa65} suite must not verify as a
	// {mldsa65}-only command: the suite is inside the signed bytes.
	a := newAuthorityFixture(t, 1, requiredDual)
	dual := a.prepare(t, 1)
	pqEnrollment := a.e
	pqEnrollment.Authority = authorityFor(1, requiredPQOnly, a.keys)
	pqKeys := narrowKeys(a.keys, requiredPQOnly)
	spliced := dual
	spliced.Command.Authority = expectedBinding(pqEnrollment)
	spliced.Signatures = slices.DeleteFunc(slices.Clone(dual.Signatures), func(s CommandSignature) bool { return s.Algorithm == AlgEd25519 })
	if verifyCommand(spliced, pqEnrollment, pqKeys) {
		t.Fatal("dual-suite ML-DSA signature verified as a PQ-only command")
	}
	// ML-DSA signature context separation: the same bytes under another context fail.
	input, _ := commandInputV2(dual.Command)
	other, err := a.signers.MLDSA65.Sign(rand.Reader, input, &mldsa.Options{Context: "hikyo/other/purpose/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if mldsa.Verify(a.keys.MLDSA65, input, other, &mldsa.Options{Context: commandLabelV2}) == nil {
		t.Fatal("ML-DSA context is not enforced")
	}
	// The v1 Ed25519 domain cannot be replayed as a v2 signature.
	v1Input, _ := commandInputV1(dual.Command)
	v1Signature, _ := a.signers.Ed25519.Sign(rand.Reader, v1Input, crypto.Hash(0))
	cross := dual
	cross.Signatures = slices.Clone(dual.Signatures)
	cross.Signatures[0].Signature = v1Signature
	if verifyCommand(cross, a.e, a.keys) {
		t.Fatal("a v1-domain Ed25519 signature verified inside a v2 command")
	}
}

func TestReplayExpiryAndSequenceUnderStrictProfile(t *testing.T) {
	a := newAuthorityFixture(t, 1, requiredDual)
	prepare := a.prepare(t, 1)
	response := a.accepted(t, prepare)
	submit := a.sign(t, Command{Sequence: 2, Action: ActionSubmit, Intent: a.intent, PlanDigest: response.PlanDigest})
	a.accepted(t, submit)

	// Replaying the earlier prepare is refused by the producer and by the
	// controller, with no write.
	if err := a.m.Send(t.Context(), prepare); !errors.Is(err, ErrConflict) {
		t.Fatal("mailbox replay accepted", err)
	}
	a.refused(t, prepare, ErrConflict)

	// A lower sequence signed afresh is still a replay of the accepted slot.
	stale := a.sign(t, Command{Sequence: 1, Action: ActionPrepare, Intent: a.intent, Changes: a.changes()})
	a.refused(t, stale, ErrConflict)

	// An accepted, expired command still resumes after restart (existing
	// behavior), but an expired new command is not accepted.
	restarted, err := NewControllerKeys(a.client, a.e, a.keys)
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = func() time.Time { return time.Now().Add(time.Hour) }
	a.plant(t, submit)
	if err := restarted.reconcile(t.Context()); err != nil {
		t.Fatalf("accepted expired command did not resume under strict: %v", err)
	}
	late := a.sign(t, Command{Sequence: 3, Action: ActionObserve, Intent: a.intent, PlanDigest: response.PlanDigest})
	a.plant(t, late)
	a.client.ClearActions()
	if err := restarted.reconcile(t.Context()); !errors.Is(err, ErrInvalid) {
		t.Fatal("expired new command accepted", err)
	}
	if got := mutations(a.fixture); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestConcurrentRolloutCannotDisplaceAcceptedOperation(t *testing.T) {
	a := newAuthorityFixture(t, 1, requiredDual)
	response := a.accepted(t, a.prepare(t, 1))
	a.accepted(t, a.sign(t, Command{Sequence: 2, Action: ActionSubmit, Intent: a.intent, PlanDigest: response.PlanDigest}))
	// A submitted plan is open until its receipt is terminal. A rival prepare
	// for another job, validly signed, is refused by the original controller and
	// by a takeover controller, with no write and the open plan kept.
	rival := a.sign(t, Command{Sequence: 3, Action: ActionPrepare, Intent: Intent{JobID: "rival", OwnerInstanceID: a.intent.OwnerInstanceID, Incarnation: a.intent.Incarnation, SnapshotID: "s2", Revision: 9, CatalogueVersion: a.intent.CatalogueVersion, ExpectedGeneration: a.intent.ExpectedGeneration + 1, Generation: a.intent.Generation + 1}, Changes: a.changes()})
	second, err := NewControllerKeys(a.client, a.e, a.keys)
	if err != nil {
		t.Fatal(err)
	}
	a.plant(t, rival)
	for _, c := range []*Controller{a.c, second} {
		a.client.ClearActions()
		if err := c.reconcile(t.Context()); !errors.Is(err, ErrConflict) {
			t.Fatal("rival displaced the open operation:", err)
		}
		if got := mutations(a.fixture); len(got) != 0 {
			t.Fatal(got)
		}
	}
	var journal controllerJournal
	if err := decode(a.secret(t, "journal").Data[journalKey], &journal); err != nil || journal.Plan == nil || journal.Plan.Plan.Intent != a.intent {
		t.Fatalf("open plan lost: %v", err)
	}
}

func TestStrictRolloutOverTheNewestExecutorPathIsRestorable(t *testing.T) {
	a := newAuthorityFixture(t, 1, requiredDual)
	response := a.accepted(t, a.prepare(t, 1))
	a.accepted(t, a.sign(t, Command{Sequence: 2, Action: ActionSubmit, Intent: a.intent, PlanDigest: response.PlanDigest}))
	restore := a.accepted(t, a.sign(t, Command{Sequence: 3, Action: ActionRestore, Intent: a.intent, PlanDigest: response.PlanDigest}))
	if restore.Receipt == nil {
		t.Fatal("restore produced no receipt")
	}
}

func journalData(t *testing.T, a authorityFixture) map[string]string {
	t.Helper()
	out := map[string]string{}
	for key, value := range a.secret(t, "journal").Data {
		out[key] = string(value)
	}
	return out
}

func TestStartupFloorRaisesAndRefusesStaleEnrollment(t *testing.T) {
	a := newAuthorityFixture(t, 3, requiredDual)
	if err := a.c.reconcileTrust(t.Context()); err != nil {
		t.Fatal(err)
	}
	var floor trustRecord
	if err := decode(a.secret(t, "journal").Data[trustKey], &floor); err != nil || floor.Generation != 3 || !sameAuthority(floor.Authority, *a.e.Authority) {
		t.Fatalf("floor not recorded: %+v %v", floor, err)
	}
	// Idempotent: a second start writes nothing.
	a.client.ClearActions()
	if err := a.c.reconcileTrust(t.Context()); err != nil || len(mutations(a.fixture)) != 0 {
		t.Fatal("equal generation rewrote the floor", err, mutations(a.fixture))
	}

	before := journalData(t, a)
	for name, mutate := range map[string]func(*Enrollment) []PublicKeys{
		"stale generation": func(e *Enrollment) []PublicKeys { e.Authority.Generation = 2; return nil },
		"compat rollback":  func(e *Enrollment) []PublicKeys { e.Authority = nil; return nil },
		"same generation other keys": func(e *Enrollment) []PublicKeys {
			_, other := newKeyMaterial(t)
			e.Authority = authorityFor(3, requiredDual, other)
			return []PublicKeys{other}
		},
	} {
		e := a.e
		authority := cloneAuthority(*a.e.Authority)
		e.Authority = &authority
		keys := a.keys
		if other := mutate(&e); other != nil {
			keys = other[0]
		}
		if e.Authority == nil {
			keys = PublicKeys{Ed25519: a.keys.Ed25519}
		}
		stale, err := NewControllerKeys(a.client, e, keys)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		a.plant(t, a.prepare(t, 1))
		a.client.ClearActions()
		if err := stale.reconcileTrust(t.Context()); !errors.Is(err, ErrTrust) {
			t.Fatalf("%s: stale trust state accepted: %v", name, err)
		}
		// The per-tick path refuses too, so a floor raised under a running
		// process (a journal restored with a higher floor) is honored.
		if err := stale.reconcile(t.Context()); !errors.Is(err, ErrTrust) {
			t.Fatalf("%s: stale controller executed: %v", name, err)
		}
		if got := mutations(a.fixture); len(got) != 0 {
			t.Fatalf("%s: stale enrollment caused mutations %v", name, got)
		}
		if after := journalData(t, a); !maps_equal(before, after) {
			t.Fatalf("%s: journal changed", name)
		}
	}
	// A higher generation is accepted and raises the floor.
	e := a.e
	e.Authority = authorityFor(4, requiredDual, a.keys)
	next, err := NewControllerKeys(a.client, e, a.keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := next.reconcileTrust(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := decode(a.secret(t, "journal").Data[trustKey], &floor); err != nil || floor.Generation != 4 {
		t.Fatal("floor not raised", floor, err)
	}
	// The previous generation is now obsolete even with the same keys.
	if err := a.c.reconcileTrust(t.Context()); !errors.Is(err, ErrTrust) {
		t.Fatal("obsolete generation resurrected authority", err)
	}
}

func maps_equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func TestCompatibilityStartupWritesNothingAndRefusesRollbackBelowFloor(t *testing.T) {
	f, e, c, _, _ := controllerFixture(t)
	f.client.ClearActions()
	if err := c.reconcileTrust(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := mutations(f); len(got) != 0 {
		t.Fatalf("compatibility startup wrote %v", got)
	}
	if _, present := f.secret(t, "journal").Data[trustKey]; present {
		t.Fatal("compatibility journal gained a trust key")
	}
	_, keys := newKeyMaterial(t)
	strictEnrollment := e
	strictEnrollment.Authority = authorityFor(1, requiredDual, keys)
	s := f.secret(t, "journal")
	putRecord(s, trustKey, trustFor(strictEnrollment))
	if _, err := f.client.CoreV1().Secrets("hikyo").Update(t.Context(), s, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcileTrust(t.Context()); !errors.Is(err, ErrTrust) {
		t.Fatal("rollback to the compatibility profile under a raised floor was accepted", err)
	}
	// Malformed floor records are refused, not treated as absent.
	s = f.secret(t, "journal")
	s.Data[trustKey] = []byte(`{"version":1}`)
	if _, err := f.client.CoreV1().Secrets("hikyo").Update(t.Context(), s, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcileTrust(t.Context()); !errors.Is(err, ErrTrust) {
		t.Fatal("malformed floor treated as absent", err)
	}
}

// transition models operator re-enrollment (DEC-20 c): a new enrollment record
// and key files, a restarted controller, the same journal and Kubernetes state.
func transition(t *testing.T, a authorityFixture, e Enrollment, signers Signers, keys PublicKeys) authorityFixture {
	t.Helper()
	return rebuild(t, a.fixture, e, signers, keys)
}

func TestTrustRolloverFromCompatibilityRetainsPlanForRestore(t *testing.T) {
	f, e, c, m, private := controllerFixture(t)
	prepare := signedFixture(t, private, e, Command{Sequence: 1, Action: ActionPrepare, Intent: f.intent, Changes: f.changes()})
	if err := m.Send(t.Context(), prepare); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	response, err := m.Response(t.Context(), prepare)
	if err != nil {
		t.Fatal(err)
	}
	submit := signedFixture(t, private, e, Command{Sequence: 2, Action: ActionSubmit, Intent: f.intent, PlanDigest: response.PlanDigest})
	if err := m.Send(t.Context(), submit); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Re-enroll: the Ed25519 key is kept, an ML-DSA key is added, generation 1.
	pq, err := mldsa.GenerateKey(mldsa.MLDSA65())
	if err != nil {
		t.Fatal(err)
	}
	signers := Signers{Ed25519: private, MLDSA65: pq}
	keys := signers.Public()
	strict := e
	strict.Authority = authorityFor(1, requiredDual, keys)
	a := rebuild(t, f, strict, signers, keys)

	// Takeover: the new controller can anchor the retained v1 journal entry.
	f.client.ClearActions()
	if err := a.c.reconcileTrust(t.Context()); err != nil {
		t.Fatalf("pre-rollover journal not anchored: %v", err)
	}
	// The pre-rollover command is still in the mailbox and was already accepted
	// and answered: it is honored as the accepted one, not as new authority.
	if err := a.c.reconcile(t.Context()); err != nil {
		t.Fatalf("accepted pre-rollover command not resumable: %v", err)
	}
	// A new legacy command is refused after the rollover.
	legacy := signedFixture(t, private, e, Command{Sequence: 3, Action: ActionRestore, Intent: f.intent, PlanDigest: response.PlanDigest})
	a.refused(t, legacy, ErrInvalid)
	// Restore under the new regime still finds the plan: nothing was lost.
	restore := a.sign(t, Command{Sequence: 3, Action: ActionRestore, Intent: f.intent, PlanDigest: response.PlanDigest})
	if got := a.accepted(t, restore); got.Receipt == nil {
		t.Fatal("restore after rollover produced no receipt")
	}
	var journal controllerJournal
	if err := decode(a.secret(t, "journal").Data[journalKey], &journal); err != nil || journal.Plan == nil || journal.Accepted.Command.Authority == nil {
		t.Fatal("journal not re-anchored on the new regime", err)
	}
}

func TestKeyRotationAndJournalResetBehaviorIsStated(t *testing.T) {
	a := newAuthorityFixture(t, 1, requiredDual)
	response := a.accepted(t, a.prepare(t, 1))
	a.accepted(t, a.sign(t, Command{Sequence: 2, Action: ActionSubmit, Intent: a.intent, PlanDigest: response.PlanDigest}))
	oldJournal := journalData(t, a)

	t.Run("classical key rotation keeps the plan", func(t *testing.T) {
		fresh, _ := newKeyMaterial(t)
		signers := Signers{Ed25519: fresh.Ed25519, MLDSA65: a.signers.MLDSA65}
		keys := signers.Public()
		e := a.e
		e.Authority = authorityFor(2, requiredDual, keys)
		b := transition(t, a, e, signers, keys)
		if err := b.c.reconcileTrust(t.Context()); err != nil {
			t.Fatalf("PQ signature should anchor a rotated classical key: %v", err)
		}
		restore := b.sign(t, Command{Sequence: 3, Action: ActionRestore, Intent: a.intent, PlanDigest: response.PlanDigest})
		if got := b.accepted(t, restore); got.Receipt == nil {
			t.Fatal("restore failed after classical rotation")
		}
	})

	t.Run("ML-DSA key replacement with a retained plan fails visibly and writes nothing", func(t *testing.T) {
		restoreJournal(t, a, oldJournal)
		fresh, _ := newKeyMaterial(t)
		signers := Signers{Ed25519: a.signers.Ed25519, MLDSA65: fresh.MLDSA65}
		keys := signers.Public()
		e := a.e
		e.Authority = authorityFor(5, requiredDual, keys)
		b := transition(t, a, e, signers, keys)
		a.client.ClearActions()
		if err := b.c.reconcileTrust(t.Context()); !errors.Is(err, ErrTrust) {
			t.Fatalf("replaced ML-DSA key anchored a journal it never signed: %v", err)
		}
		if got := mutations(a.fixture); len(got) != 0 {
			t.Fatal(got)
		}
		b.refused(t, b.sign(t, Command{Sequence: 9, Action: ActionRestore, Intent: a.intent, PlanDigest: response.PlanDigest}), ErrTrust)
		if !maps_equal(oldJournal, journalData(t, a)) {
			t.Fatal("refusal altered the journal")
		}
	})

	t.Run("enrollment id replacement fails the same way", func(t *testing.T) {
		restoreJournal(t, a, oldJournal)
		e := a.e
		e.ID = "enrollment-2"
		b := transition(t, a, e, a.signers, a.keys)
		if err := b.c.reconcileTrust(t.Context()); !errors.Is(err, ErrTrust) {
			t.Fatal(err)
		}
	})

	t.Run("emptying only journal.json drops the plan but keeps the floor", func(t *testing.T) {
		restoreJournal(t, a, oldJournal)
		s := a.secret(t, "journal")
		delete(s.Data, journalKey)
		if _, err := a.client.CoreV1().Secrets("hikyo").Update(t.Context(), s, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		// Restore needs the plan: it is gone, so a signed restore is refused
		// without mutation. This is the stated cost of resetting the journal.
		a.refused(t, a.sign(t, Command{Sequence: 3, Action: ActionRestore, Intent: a.intent, PlanDigest: response.PlanDigest}), ErrConflict)
		// The floor survived the reset, so a rollback to compatibility is refused.
		compat := a.e
		compat.Authority = nil
		c, err := NewControllerKeys(a.client, compat, PublicKeys{Ed25519: a.keys.Ed25519})
		if err != nil {
			t.Fatal(err)
		}
		if err := c.reconcileTrust(t.Context()); !errors.Is(err, ErrTrust) {
			t.Fatal("floor lost when only journal.json was emptied", err)
		}
	})

	t.Run("emptying the whole journal Secret erases the floor: registered residual E-14", func(t *testing.T) {
		restoreJournal(t, a, oldJournal)
		s := a.secret(t, "journal")
		s.Data = nil
		if _, err := a.client.CoreV1().Secrets("hikyo").Update(t.Context(), s, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		// Nothing in the cluster remembers a higher generation after this:
		// journal write is authority over the floor, so a stale enrollment
		// would be accepted. The runbook forbids this and the register records it.
		compat := a.e
		compat.Authority = nil
		c, err := NewControllerKeys(a.client, compat, PublicKeys{Ed25519: a.keys.Ed25519})
		if err != nil {
			t.Fatal(err)
		}
		if err := c.reconcileTrust(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}

func restoreJournal(t *testing.T, a authorityFixture, data map[string]string) {
	t.Helper()
	s := a.secret(t, "journal")
	s.Data = map[string][]byte{}
	for key, value := range data {
		s.Data[key] = []byte(value)
	}
	if _, err := a.client.CoreV1().Secrets("hikyo").Update(t.Context(), s, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestInterruptedRolloverRefusesPreRolloverCommandsWithoutMutation(t *testing.T) {
	// The operator re-enrolls while a signed command is queued but not accepted.
	f, e, c, m, private := controllerFixture(t)
	queued := signedFixture(t, private, e, Command{Sequence: 1, Action: ActionPrepare, Intent: f.intent, Changes: f.changes()})
	if err := m.Send(t.Context(), queued); err != nil {
		t.Fatal(err)
	}
	_ = c
	pq, _ := mldsa.GenerateKey(mldsa.MLDSA65())
	signers := Signers{Ed25519: private, MLDSA65: pq}
	strict := e
	strict.Authority = authorityFor(1, requiredDual, signers.Public())
	a := rebuild(t, f, strict, signers, signers.Public())
	f.client.ClearActions()
	if err := a.c.reconcile(t.Context()); !errors.Is(err, ErrInvalid) {
		t.Fatal("queued pre-rollover command executed under the new trust state", err)
	}
	if got := mutations(f); len(got) != 0 {
		t.Fatal(got)
	}
	// Interrupted: the controller restarts back on the old enrollment. The floor
	// is only raised when the new regime reaches a journal write, so the old
	// regime is still consistent and the queued command runs.
	old, err := NewController(f.client, e, private.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	if err := old.reconcile(t.Context()); err != nil {
		t.Fatalf("an interrupted rollover (never reached a write) broke the old regime: %v", err)
	}
	// After the new regime has written its floor, going back is refused.
	if err := a.c.reconcileTrust(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := old.reconcileTrust(t.Context()); !errors.Is(err, ErrTrust) {
		t.Fatal("rollback to the old regime after the floor was raised was accepted", err)
	}
}

func TestControllerTakeoverRunsTrustReconciliationBeforeCommands(t *testing.T) {
	a := newAuthorityFixture(t, 2, requiredPQOnly)
	// Pod replacement: the new Pod UID takes custody; its first act is the
	// trust reconciliation, which records the floor with no command involved.
	pod, err := a.client.CoreV1().Pods("hikyo").Get(t.Context(), a.e.ExecutorPod, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pod.UID = "pod-2"
	if err := a.client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("pods"), pod, "hikyo"); err != nil {
		t.Fatal(err)
	}
	owner := &custody{client: a.client, enrollment: a.e, podUID: "pod-2"}
	if err := owner.acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	guarded := Controller{client: custodyClient{Interface: a.client, owner: owner}, enrollment: a.e, keys: a.keys, executor: a.c.executor, now: time.Now}
	if err := guarded.reconcileTrust(t.Context()); err != nil {
		t.Fatal(err)
	}
	var floor trustRecord
	if err := decode(a.secret(t, "journal").Data[trustKey], &floor); err != nil || floor.Generation != 2 {
		t.Fatal("takeover did not record the floor", err)
	}
	if got := a.secret(t, "journal").Annotations[custodyAnnotation]; got == "" {
		t.Fatal("floor write did not carry the custody epoch the admission policy requires")
	}
}

func TestStrictRecordSizesMatchDocumentedBounds(t *testing.T) {
	a := newAuthorityFixture(t, 1, requiredDual)
	signed := a.prepare(t, 1)
	sizes := map[string]int{}
	for _, s := range signed.Signatures {
		sizes[s.Algorithm] = len(s.Signature)
	}
	if sizes[AlgEd25519] != 64 || sizes[AlgMLDSA65] != 3309 || mldsa.MLDSA65().PublicKeySize() != 1952 {
		t.Fatal("actual key or signature sizes differ from the documented bounds", sizes)
	}
	raw := mustJSON(signed)
	if len(raw) > maxRecordBytes {
		t.Fatal("strict command exceeds the record cap")
	}
	t.Logf("strict dual command record: %d bytes (cap %d); signatures %v", len(raw), maxRecordBytes, sizes)
}

func BenchmarkSignAndVerify(b *testing.B) {
	signers, keys := newKeyMaterial(b)
	command := goldenCommand()
	command.Intent.JobID = "bench"
	for name, required := range map[string][]string{"compat-ed25519": nil, "dual": requiredDual, "pq-only": requiredPQOnly} {
		e := Enrollment{ID: command.EnrollmentID, Target: Target{Namespace: "hikyo"}}
		s := Signers{Ed25519: signers.Ed25519}
		k := PublicKeys{Ed25519: keys.Ed25519}
		if required != nil {
			e.Authority = authorityFor(1, required, keys)
			s, k = narrowSigners(signers, required), narrowKeys(keys, required)
		}
		signed, err := SignCommandFor(b.Context(), e, s, command)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(name+"/sign", func(b *testing.B) {
			for b.Loop() {
				if _, err := SignCommandFor(b.Context(), e, s, command); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(name+"/verify", func(b *testing.B) {
			input, _ := commandInputV2(signed.Command)
			if required == nil {
				input, _ = commandInputV1(signed.Command)
			}
			b.ReportMetric(float64(len(mustJSON(signed))), "record-bytes")
			for b.Loop() {
				if required == nil {
					ed25519.Verify(k.Ed25519, input, signed.Signature)
					continue
				}
				for _, sig := range signed.Signatures {
					verifyAlgorithm(sig.Algorithm, k, input, sig.Signature)
				}
			}
		})
	}
}

func TestDualToPQOnlyKeepsTheRetainedPlanAndRefusesTheRetiredAlgorithm(t *testing.T) {
	a := newAuthorityFixture(t, 1, requiredDual)
	response := a.accepted(t, a.prepare(t, 1))
	a.accepted(t, a.sign(t, Command{Sequence: 2, Action: ActionSubmit, Intent: a.intent, PlanDigest: response.PlanDigest}))

	e := a.e
	e.Authority = authorityFor(2, requiredPQOnly, a.keys)
	b := transition(t, a, e, narrowSigners(a.signers, requiredPQOnly), narrowKeys(a.keys, requiredPQOnly))
	if err := b.c.reconcileTrust(t.Context()); err != nil {
		t.Fatalf("the dual entry should anchor through its ML-DSA signature: %v", err)
	}
	// The retired classical algorithm no longer authorizes anything: the old
	// dual regime's commands and a lone Ed25519 signature are both refused.
	oldRegime := a
	oldRegime.c = b.c
	b.refused(t, oldRegime.sign(t, Command{Sequence: 3, Action: ActionRestore, Intent: a.intent, PlanDigest: response.PlanDigest}), ErrInvalid)
	if got := b.accepted(t, b.sign(t, Command{Sequence: 3, Action: ActionRestore, Intent: a.intent, PlanDigest: response.PlanDigest})); got.Receipt == nil {
		t.Fatal("restore after retiring Ed25519 produced no receipt")
	}
	// Going back to the dual profile at a lower generation, or to the same
	// generation with Ed25519 re-added, is refused by the floor.
	back := a.e
	back.Authority = authorityFor(1, requiredDual, a.keys)
	old, err := NewControllerKeys(a.client, back, a.keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.reconcileTrust(t.Context()); !errors.Is(err, ErrTrust) {
		t.Fatal("re-enrolling the retired classical key at an old generation was accepted", err)
	}
}

func TestEnrollmentTargetEditKeepsAnchorButRefusesOldTargetCommands(t *testing.T) {
	a := newAuthorityFixture(t, 1, requiredDual)
	response := a.accepted(t, a.prepare(t, 1))
	queued := a.sign(t, Command{Sequence: 2, Action: ActionSubmit, Intent: a.intent, PlanDigest: response.PlanDigest})

	// The operator adds a source alias to the enrolled target at the same trust
	// generation and keys. The target digest in the signed binding changes.
	e := a.e
	e.Target.RootSources = map[string]SecretSource{"added": {Name: "added-root", Key: "root-key"}}
	b := transition(t, a, e, a.signers, a.keys)
	if err := b.c.reconcileTrust(t.Context()); err != nil {
		t.Fatalf("an authentic retained entry became unanchorable after a target edit: %v", err)
	}
	// A command bound to the old target is refused with no write.
	b.refused(t, queued, ErrInvalid)
	// One signed for the new target is accepted by the verifier; the plan's own
	// target check (existing behavior) then decides whether the old plan is usable.
	fresh := b.sign(t, Command{Sequence: 2, Action: ActionSubmit, Intent: a.intent, PlanDigest: response.PlanDigest})
	if !verifyCommand(fresh, e, b.keys) || verifyCommand(queued, e, b.keys) {
		t.Fatal("target digest binding not enforced on fresh commands")
	}
}
