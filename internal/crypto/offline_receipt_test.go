package crypto

import (
	"bytes"
	"testing"
)

func TestOfflineReceiptScopePurposeAndTampering(t *testing.T) {
	ks := newMemStore()
	kr, err := LoadKeyring(t.Context(), ks, newRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	row, err := ks.ActiveTier3(t.Context(), PurposeToken, "", "")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := kr.DeliveryTokenSnapshot(row, "org", "project", "env")
	if err != nil {
		t.Fatal(err)
	}
	defer signer.Close()
	payload := []byte("server selected claims")
	signature := signer.Sign(payload)
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
		verifier, err := kr.DeliveryTokenSnapshot(row, tt.org, tt.project, tt.env)
		if err != nil {
			t.Fatal(err)
		}
		valid := verifier.Verify(tt.claims, tt.signature)
		verifier.Close()
		if valid != tt.valid {
			t.Fatalf("receipt verify = %v; want %v", valid, tt.valid)
		}
	}
	change, err := kr.ChangeToken("org", "project", "env", payload)
	if err != nil {
		t.Fatal(err)
	}
	if signer.Verify(payload, change) {
		t.Fatal("change token substituted for receipt")
	}
}

func TestOfflineReceiptSnapshotImmutableAcrossRotation(t *testing.T) {
	ks := newMemStore()
	kr, err := LoadKeyring(t.Context(), ks, newRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	row, err := ks.ActiveTier3(t.Context(), PurposeToken, "", "")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := kr.DeliveryTokenSnapshot(row, "org", "project", "env")
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	payload := []byte("immutable authorized delivery")
	before := []string{snapshot.Sign(payload), snapshot.ChangeToken(payload), snapshot.DeliveryCursor(payload)}
	legacyChange, err := kr.ChangeToken("org", "project", "env", payload)
	if err != nil {
		t.Fatal(err)
	}
	legacyCursor, err := kr.DeliveryCursor("org", "project", "env", payload)
	if err != nil {
		t.Fatal(err)
	}
	if before[1] != legacyChange || before[2] != legacyCursor || before[0] == before[1] || before[0] == before[2] || before[1] == before[2] {
		t.Fatal("snapshot changed the wire derivation or collapsed purpose separation")
	}
	next, adopt, abort, err := kr.PrepareTokenKeyRotation()
	if err != nil {
		t.Fatal(err)
	}
	defer abort()
	adopt()
	fresh, err := kr.DeliveryTokenSnapshot(next, "org", "project", "env")
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	after := []string{snapshot.Sign(payload), snapshot.ChangeToken(payload), snapshot.DeliveryCursor(payload)}
	rotated := []string{fresh.Sign(payload), fresh.ChangeToken(payload), fresh.DeliveryCursor(payload)}
	for i := range before {
		if after[i] != before[i] || rotated[i] == before[i] {
			t.Fatal("rotation changed an in-flight snapshot or retained its key")
		}
	}
	for _, bad := range []WrappedKey{
		{Purpose: PurposeScanning, Version: 1},
		{Purpose: PurposeToken, Version: 1, OrgID: "foreign"},
		{Purpose: PurposeToken, Version: 1, ProjectID: "foreign"},
		{Purpose: PurposeToken},
	} {
		if _, err := kr.DeliveryTokenSnapshot(bad, "org", "project", "env"); err == nil {
			t.Fatal("non-root-token row accepted")
		}
	}
	snapshot.Close()
	for _, key := range [][]byte{snapshot.receipt, snapshot.change, snapshot.cursor} {
		if !bytes.Equal(key, make([]byte, KeySize)) {
			t.Fatal("snapshot retained key material after transaction exit")
		}
	}
}
