package crypto

import (
	"bytes"
	"testing"
)

func TestOfflineReceiptScopePurposeAndTampering(t *testing.T) {
	kr := &Keyring{}
	kr.token.adopt(keyHandle{key: bytes.Repeat([]byte{0x42}, KeySize)})
	payload := []byte("server selected claims")
	signature, err := kr.OfflineSnapshotReceipt("org", "project", "env", payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		org, project, env string
		claims            []byte
		signature         string
		valid             bool
	}{
		{"org", "project", "env", payload, signature, true},
		{"foreign", "project", "env", payload, signature, false},
		{"org", "foreign", "env", payload, signature, false},
		{"org", "project", "foreign", payload, signature, false},
		{"org", "project", "env", []byte("modified claims"), signature, false},
		{"org", "project", "env", payload, signature + "A", false},
	} {
		valid, err := kr.VerifyOfflineSnapshotReceipt(tt.org, tt.project, tt.env, tt.claims, tt.signature)
		if err != nil || valid != tt.valid {
			t.Fatalf("receipt verify = %v, %v; want %v", valid, err, tt.valid)
		}
	}
	change, err := kr.ChangeToken("org", "project", "env", payload)
	if err != nil {
		t.Fatal(err)
	}
	if valid, err := kr.VerifyOfflineSnapshotReceipt("org", "project", "env", payload, change); err != nil || valid {
		t.Fatalf("change token substituted for receipt: %v %v", valid, err)
	}
}
