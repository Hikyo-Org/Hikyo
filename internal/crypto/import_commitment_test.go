package crypto

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"testing"
)

func TestImportCommitmentUsesPrivateKeyAndV2Domain(t *testing.T) {
	key, err := NewImportCommitmentKey()
	if err != nil {
		t.Fatal(err)
	}
	defer Zero(key)
	if len(key) != KeySize {
		t.Fatal("private commitment key is not 256 bits")
	}
	content := []byte("canonical secret content")
	got, err := ImportValuesCommitment(key, content)
	if err != nil {
		t.Fatal(err)
	}
	reference := hmac.New(sha256.New, key)
	_, _ = reference.Write([]byte("hikyo-import-values-commitment-v2\x00"))
	_, _ = reference.Write(content)
	if !hmac.Equal(got, reference.Sum(nil)) {
		t.Fatal("commitment does not implement the v2 domain-separated contract")
	}
	unscoped := hmac.New(sha256.New, key)
	_, _ = unscoped.Write(content)
	if bytes.Equal(got, unscoped.Sum(nil)) {
		t.Fatal("commitment omitted domain separation")
	}
	if _, err := ImportValuesCommitment(key[:len(key)-1], content); err == nil {
		t.Fatal("commitment accepted a short private key")
	}
}
