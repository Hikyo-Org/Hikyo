package crypto

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestMCPCursorSealerHidesAndAuthenticatesPayload(t *testing.T) {
	sealer := &MCPCursorSealer{derive: func(context.Context) ([]byte, error) { return make([]byte, KeySize), nil }}
	payload := []byte(`{"k":"tenant-private-environment-name"}`)
	token, err := sealer.Seal(t.Context(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(token, "tenant-private-environment-name") {
		t.Fatalf("cursor exposed tenant name: %q", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "tenant-private-environment-name") {
		t.Fatalf("decoded cursor exposed tenant name: %q", raw)
	}
	opened, err := sealer.Open(t.Context(), token)
	if err != nil || string(opened) != string(payload) {
		t.Fatalf("cursor open = %q, %v", opened, err)
	}
	raw[len(raw)-1] ^= 1
	if _, err := sealer.Open(t.Context(), base64.RawURLEncoding.EncodeToString(raw)); err == nil {
		t.Fatal("tampered cursor was accepted")
	}
}

func TestMCPCursorSealerUsesLiveTokenKeyAfterRotation(t *testing.T) {
	kr, err := LoadKeyring(context.Background(), newMemStore(), newRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	sealer, err := kr.MCPCursorSealer()
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"page":1}`)
	before, err := sealer.Seal(t.Context(), payload)
	if err != nil {
		t.Fatal(err)
	}

	_, adopt, abort, err := kr.PrepareTokenKeyRotation()
	if err != nil {
		t.Fatal(err)
	}
	defer abort()
	adopt()

	if _, err := sealer.Open(t.Context(), before); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("pre-rotation cursor after rotation = %v, want ErrCursorInvalid", err)
	}
	after, err := sealer.Seal(t.Context(), payload)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := sealer.Open(t.Context(), after)
	if err != nil || string(opened) != string(payload) {
		t.Fatalf("post-rotation cursor open = %q, %v", opened, err)
	}
}

func TestMCPCursorSealerRefreshesTokenKeyAfterCrossReplicaRotation(t *testing.T) {
	ctx := context.Background()
	ks := newMemStore()
	root := newRoot(t)
	rootCopy := bytes.Clone(root)
	rotating, err := LoadKeyring(ctx, ks, root)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := LoadKeyring(ctx, ks, rootCopy)
	if err != nil {
		t.Fatal(err)
	}
	rotatingSealer, err := rotating.MCPCursorSealer()
	if err != nil {
		t.Fatal(err)
	}
	staleSealer, err := stale.MCPCursorSealer()
	if err != nil {
		t.Fatal(err)
	}

	row, adopt, abort, err := rotating.PrepareTokenKeyRotation()
	if err != nil {
		t.Fatal(err)
	}
	defer abort()
	ks.mu.Lock()
	key := t3key(PurposeToken, "", "")
	ks.tier3[key] = append(ks.tier3[key], row)
	ks.mu.Unlock()
	adopt()

	payload := []byte(`{"page":2}`)
	cursor, err := rotatingSealer.Seal(ctx, payload)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := staleSealer.Open(ctx, cursor)
	if err != nil || string(opened) != string(payload) {
		t.Fatalf("stale replica open after shared rotation = %q, %v", opened, err)
	}
}
