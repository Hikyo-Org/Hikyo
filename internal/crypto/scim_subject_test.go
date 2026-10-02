package crypto

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestSCIMSubjectDigestCannotExposeGuessableSubject(t *testing.T) {
	const subject = "employee-123"
	first, err := SCIMSubjectDigest("org", "binding", "issuer", subject)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SCIMSubjectDigest("org", "binding", "issuer", subject)
	if err != nil {
		t.Fatal(err)
	}
	raw := sha256.Sum256([]byte(subject))
	if first == second || first == hex.EncodeToString(raw[:]) {
		t.Fatal("audit subject exposes a stable guessing/equality oracle")
	}
	if decoded, err := hex.DecodeString(first); err != nil || len(decoded) != sha256.Size {
		t.Fatalf("v1 digest spelling changed: %q", first)
	}
}

func TestSCIMSubjectDigestDomainAndScope(t *testing.T) {
	key := bytes.Repeat([]byte{1}, KeySize)
	want := scimSubjectDigest(key, "a", "bc", "issuer", "subject")
	for _, tuple := range [][4]string{{"ab", "c", "issuer", "subject"}, {"a", "bc", "other", "subject"}, {"a", "bc", "issuer", "other"}} {
		if got := scimSubjectDigest(key, tuple[0], tuple[1], tuple[2], tuple[3]); got == want {
			t.Fatal("scope/subject collision")
		}
	}
	if got := scimSubjectDigest(bytes.Repeat([]byte{2}, KeySize), "a", "bc", "issuer", "subject"); got == want {
		t.Fatal("private key ignored")
	}
}
