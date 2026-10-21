package pairing

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	srp "github.com/ProtonMail/go-srp"
)

const testPhrase = "abacus abacus abacus abacus abacus abacus"

func testContext() Context {
	return Context{InviteID: "pinv_test", Origin: "https://serving.example",
		SPKIPin:             base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)),
		RequesterThumbprint: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)),
		InviteSalt:          bytes.Repeat([]byte{3}, 32)}
}

func TestPhraseGenerationAndCustomSecretValidation(t *testing.T) {
	for _, count := range []int{1, 5, PhraseWords, MaxPhraseWords} {
		phrase, err := GeneratePhraseWords(count)
		if err != nil || len(strings.Fields(phrase)) != count || ValidatePhrase(phrase) != nil {
			t.Fatalf("generated phrase length%d: %v", count, err)
		}
	}
	for _, count := range []int{0, -1, MaxPhraseWords + 1} {
		if _, err := GeneratePhraseWords(count); err == nil {
			t.Fatal("out-of-bounds generator input accepted")
		}
	}
	for _, phrase := range []string{"a", "four", "five!", "  Exact SPACE ", "任意の秘密", "a\x00b", strings.Repeat("x", MaxPhraseBytes)} {
		if err := ValidatePhrase(phrase); err != nil {
			t.Fatalf("user-selected secret was refused: %v", err)
		}
	}
	for _, phrase := range []string{"", strings.Repeat("x", MaxPhraseBytes+1), string([]byte{0xff})} {
		if ValidatePhrase(phrase) == nil {
			t.Fatal("empty, oversized or invalid UTF-8 secret accepted")
		}
	}
}

func TestSRPAuthenticatesEntireExactCustomSecret(t *testing.T) {
	cases := []struct {
		name, secret, other string
	}{
		{"four characters", "aB7!", "ab7!"},
		{"five characters", "aB7!x", "aB7!X"},
		{"whitespace", "  secret ", "secret"},
		{"Unicode", "秘密é", "秘密e\u0301"},
		{"beyond bcrypt limit", strings.Repeat("x", 72) + "A", strings.Repeat("x", 72) + "B"},
		{"maximum input", strings.Repeat("x", MaxPhraseBytes-1) + "A", strings.Repeat("x", MaxPhraseBytes-1) + "B"},
		{"embedded NUL", "ab\x00CD", "ab\x00Cd"},
		{"cyclic NUL alias", "a", "a\x00a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			context := testContext()
			enrollment, err := EnrollPhrase(tc.secret, context)
			if err != nil {
				t.Fatal(err)
			}
			challenge, err := StartServer(enrollment)
			if err != nil {
				t.Fatal(err)
			}
			proofs, err := ClientProof(tc.secret, context, challenge.Ephemeral)
			if err != nil {
				t.Fatal(err)
			}
			serverProof, err := FinishServer(enrollment, challenge.Secret, proofs.ClientEphemeral, proofs.ClientProof)
			if err != nil || VerifyServerProof(proofs.ExpectedServerProof, serverProof) != nil {
				t.Fatal("exact custom secret did not mutually authenticate")
			}
			altered, err := ClientProof(tc.other, context, challenge.Ephemeral)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := FinishServer(enrollment, challenge.Secret, altered.ClientEphemeral, altered.ClientProof); err == nil {
				t.Fatal("a different custom secret authenticated")
			}
		})
	}
}

func TestSRPContextSaltPreservesAll128Bits(t *testing.T) {
	salt := bytes.Repeat([]byte{1}, 16)
	auth, err := srp.NewAuthForVerifier([]byte(testPhrase), signedModulus, salt)
	if err != nil {
		t.Fatal(err)
	}
	// The final byte proves the KDF does not truncate to Proton's conventional
	// ten application salt bytes or to fewer than 128 bits.
	salt[15] ^= 1
	changed, err := srp.NewAuthForVerifier([]byte(testPhrase), signedModulus, salt)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(auth.HashedPassword, changed.HashedPassword) {
		t.Fatal("KDF discarded context salt bits")
	}
}

func TestSRPRoundTripFromSerializedState(t *testing.T) {
	context := testContext()
	enrollment, err := EnrollPhrase(testPhrase, context)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := StartServer(enrollment)
	if err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(struct {
		Enrollment Enrollment
		Challenge  ServerChallenge
	}{enrollment, challenge})
	if err != nil {
		t.Fatal(err)
	}
	var restored struct {
		Enrollment Enrollment
		Challenge  ServerChallenge
	}
	if err := json.Unmarshal(state, &restored); err != nil {
		t.Fatal(err)
	}
	proofs, err := ClientProof(testPhrase, context, challenge.Ephemeral)
	if err != nil {
		t.Fatal(err)
	}
	serverProof, err := FinishServer(restored.Enrollment, restored.Challenge.Secret, proofs.ClientEphemeral, proofs.ClientProof)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyServerProof(proofs.ExpectedServerProof, serverProof); err != nil {
		t.Fatal(err)
	}
	serverProof[0] ^= 1
	if err := VerifyServerProof(proofs.ExpectedServerProof, serverProof); err == nil {
		t.Fatal("modified server proof accepted")
	}

	other, err := StartServer(enrollment)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FinishServer(enrollment, other.Secret, proofs.ClientEphemeral, proofs.ClientProof); err == nil {
		t.Fatal("proof replayed into a different exchange")
	}
	// Same-exchange replay is intentionally prevented by the caller's atomic
	// exchange/invitation consumption, not this deterministic crypto adapter.
}

func TestSRPRejectsWrongPhraseAndContextSubstitution(t *testing.T) {
	context := testContext()
	enrollment, err := EnrollPhrase(testPhrase, context)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := StartServer(enrollment)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Context) string{
		"phrase": func(*Context) string { return "ability abacus abacus abacus abacus abacus" },
		"invite": func(c *Context) string { c.InviteID = "pinv_other"; return testPhrase },
		"origin": func(c *Context) string { c.Origin = "https://attacker.example"; return testPhrase },
		"pin": func(c *Context) string {
			c.SPKIPin = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
			return testPhrase
		},
		"key": func(c *Context) string {
			c.RequesterThumbprint = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))
			return testPhrase
		},
		"salt": func(c *Context) string { c.InviteSalt = bytes.Repeat([]byte{6}, 32); return testPhrase },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			altered := context
			phrase := change(&altered)
			proofs, err := ClientProof(phrase, altered, challenge.Ephemeral)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := FinishServer(enrollment, challenge.Secret, proofs.ClientEphemeral, proofs.ClientProof); err == nil {
				t.Fatal("substituted phrase/context authenticated")
			}
		})
	}
}

func TestSRPInvalidInputs(t *testing.T) {
	if _, err := ContextSalt(Context{}); err == nil {
		t.Fatal("empty context accepted")
	}
	if _, err := StartServer(Enrollment{}); err == nil {
		t.Fatal("invalid enrollment accepted")
	}
	if _, err := ClientProof(testPhrase, testContext(), nil); err == nil {
		t.Fatal("invalid server ephemeral accepted")
	}
	if _, err := FinishServer(Enrollment{}, nil, nil, nil); err == nil {
		t.Fatal("invalid exchange accepted")
	}
	if err := VerifyServerProof(nil, nil); err == nil {
		t.Fatal("empty server proof accepted")
	}
}
