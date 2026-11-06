package crypto

import (
	"bytes"
	"strings"
	"testing"
)

// The accepted grammar must be EXACTLY the emittable one: every mint round-
// trips, and a body one byte too long or too short is refused. This is the
// property the char-count bound could not hold — leading zero bytes shorten
// the body, so a fixed window admitted lengths no mint emits and rejected the
// rare short body a real mint does.
func TestParseArtifactAcceptsOnlyEmittableBodies(t *testing.T) {
	// Every mint parses, across enough draws to hit occasional leading-zero
	// (hence shorter) bodies.
	for range 2000 {
		v, _, err := NewArtifact(ArtifactCLISession)
		if err != nil {
			t.Fatal(err)
		}
		if err := ParseArtifact(v, ArtifactCLISession); err != nil {
			t.Fatalf("a freshly minted artifact was refused: %q -> %v", v, err)
		}
	}

	// A body that decodes to the wrong number of bytes is refused, even with a
	// correct checksum: 33 bytes and 31 bytes both bracket the 32 a mint emits.
	for _, raw := range [][]byte{make([]byte, bodyBytes+1), make([]byte, bodyBytes-1)} {
		for i := range raw {
			raw[i] = byte(i + 1)
		}
		body := base62(raw)
		v := "hik_" + artifactFormatVersion + "_" + string(ArtifactCLISession) + "_" + body + checksum(body)
		if err := ParseArtifact(v, ArtifactCLISession); err == nil {
			t.Fatalf("a %d-byte body was accepted, only %d is emittable", len(raw), bodyBytes)
		}
	}

	// A non-canonical encoding (an extra leading zero) is refused even though
	// it decodes to the right bytes.
	v, _, _ := NewArtifact(ArtifactBootstrap)
	// Splice an extra '0' into the body: same value, longer string.
	prefix := "hik_" + artifactFormatVersion + "_" + string(ArtifactBootstrap) + "_"
	payload := v[len(prefix):]
	body, sum := payload[:len(payload)-checksumChars], payload[len(payload)-checksumChars:]
	nonCanonical := prefix + "0" + body + sum
	if err := ParseArtifact(nonCanonical, ArtifactBootstrap); err == nil {
		t.Fatal("a non-canonical encoding was accepted")
	}
}

func TestParseArtifactRefusesOversizedBodyBeforeDecoding(t *testing.T) {
	body := strings.Repeat("z", maxBodyChars+1)
	value := "hik_" + artifactFormatVersion + "_" + string(ArtifactCLISession) + "_" + body + checksum(body)
	if err := ParseArtifact(value, ArtifactCLISession); err == nil {
		t.Fatal("oversized artifact body was accepted")
	}
}

// The sign-up verification token (social-signin spec 2.4, #605) rides the one
// grammar beside the handoff pair: pinned spelling, round trip, and refusal
// under any other type, so a `su` value can never be presented as `hs`/`hc`
// or the reverse.
func TestSignupArtifactGrammar(t *testing.T) {
	pins := map[ArtifactType]string{ArtifactHandoffState: "hs", ArtifactHandoffCode: "hc", ArtifactSignup: "su"}
	for typ, spelling := range pins {
		if string(typ) != spelling {
			t.Fatalf("artifact type %q, pinned spelling %q", typ, spelling)
		}
	}
	v, verifier, err := NewArtifact(ArtifactSignup)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v, "hik_1_su_") {
		t.Fatalf("sign-up token %q does not carry the su type", v)
	}
	if !bytes.Equal(verifier, ArtifactVerifier(v)) {
		t.Fatal("the stored verifier is not the hash of the value")
	}
	if err := ParseArtifact(v, ArtifactSignup); err != nil {
		t.Fatalf("a minted su artifact was refused: %v", err)
	}
	for _, other := range []ArtifactType{ArtifactHandoffState, ArtifactHandoffCode, ArtifactBootstrap} {
		if ParseArtifact(v, other) == nil {
			t.Fatalf("an su artifact parsed as %q", other)
		}
		o, _, err := NewArtifact(other)
		if err != nil {
			t.Fatal(err)
		}
		if ParseArtifact(o, ArtifactSignup) == nil {
			t.Fatalf("a %q artifact parsed as su", other)
		}
	}
}

// The email-invitation token (#1207) rides the one grammar: pinned spelling,
// round trip, refusal under any other type, and the `hik_` redaction family.
func TestEmailInvitationArtifactGrammar(t *testing.T) {
	if string(ArtifactEmailInvitation) != "ei" {
		t.Fatalf("artifact type %q, pinned spelling %q", ArtifactEmailInvitation, "ei")
	}
	v, verifier, err := NewArtifact(ArtifactEmailInvitation)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v, "hik_1_ei_") {
		t.Fatalf("email invitation token %q does not carry the ei type", v)
	}
	if !bytes.Equal(verifier, ArtifactVerifier(v)) {
		t.Fatal("the stored verifier is not the hash of the value")
	}
	if err := ParseArtifact(v, ArtifactEmailInvitation); err != nil {
		t.Fatalf("a minted ei artifact was refused: %v", err)
	}
	for _, other := range []ArtifactType{ArtifactSignup, ArtifactHandoffCode, ArtifactBootstrap} {
		if ParseArtifact(v, other) == nil {
			t.Fatalf("an ei artifact parsed as %q", other)
		}
		o, _, err := NewArtifact(other)
		if err != nil {
			t.Fatal(err)
		}
		if ParseArtifact(o, ArtifactEmailInvitation) == nil {
			t.Fatalf("a %q artifact parsed as ei", other)
		}
	}
}
