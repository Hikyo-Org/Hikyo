package crypto

import (
	"crypto/rand"
	"fmt"
	"testing"
)

// BenchmarkEnvelope measures sealing and opening one value record, the
// per-value cost every encrypted read and write pays.
func BenchmarkEnvelope(b *testing.B) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		b.Fatal(err)
	}
	keyID := []byte("dek_bench")
	aad := testValueAAD()
	for _, size := range []int{32, 4 * 1024} {
		plaintext := make([]byte, size)
		if _, err := rand.Read(plaintext); err != nil {
			b.Fatal(err)
		}
		record, err := seal(rand.Reader, key, keyID, 1, aad, plaintext)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("seal/%dB", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := seal(rand.Reader, key, keyID, 1, aad, plaintext); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("open/%dB", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := open(key, keyID, 1, aad, record); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
