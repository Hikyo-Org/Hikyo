package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

// GitHub's secret-upload contract is a libsodium sealed box (X25519 plus
// XSalsa20-Poly1305, ephemeral sender key, no sender authentication). Pin the
// wire shape so a refactor cannot swap in another envelope: 32-byte ephemeral
// key, 16-byte tag, so exactly 48 bytes over the plaintext (crypto_box_SEALBYTES).
func TestSealAnonymousBoxIsTheLibsodiumSealedBoxShape(t *testing.T) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	value := []byte("deploy-token")
	first, err := SealAnonymousBox(value, *pub)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SealAnonymousBox(value, *pub)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(value)+48 {
		t.Fatalf("sealed length = %d, want %d", len(first), len(value)+48)
	}
	if bytes.Equal(first, second) {
		t.Fatal("two seals of one value are identical: ephemeral key not fresh")
	}
	opened, ok := box.OpenAnonymous(nil, first, pub, priv)
	if !ok || !bytes.Equal(opened, value) {
		t.Fatalf("OpenAnonymous = %q, %v", opened, ok)
	}
	flipped := bytes.Clone(first)
	flipped[len(flipped)-1] ^= 1
	if _, ok := box.OpenAnonymous(nil, flipped, pub, priv); ok {
		t.Fatal("a modified sealed box opened")
	}
}
