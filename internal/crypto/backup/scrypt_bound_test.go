package backup_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
)

func TestScryptUntrustedWorkFactorRefusedBeforeDerivation(t *testing.T) {
	// This header is syntactically valid but has no authentic MAC or wrapped
	// key. Refusal must precede the attacker's requested 4 GiB KDF allocation.
	zero := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	container := []byte("age-encryption.org/v1\n-> scrypt AAAAAAAAAAAAAAAAAAAAAA 22\n" + zero + "\n--- " + zero + "\n")
	_, err := extract(backup.Unlock{Passphrase: "operator-passphrase"}, container)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("untrusted excessive work factor was not refused at the KDF bound: %v", err)
	}
}
