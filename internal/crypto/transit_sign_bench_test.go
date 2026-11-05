package crypto

import (
	"strconv"
	"testing"
)

// Baseline for the Transit signing algorithms (post-quantum migration DEC-17:
// the owner sets thresholds from a measured baseline; none is set here). The
// custody providers open the derived key for every operation, so "sign" below
// includes the key derivation and expansion a real request pays, and "verify"
// includes parsing the stored public key. Sizes are reported as metrics.
func BenchmarkTransitSignAndVerify(b *testing.B) {
	for _, alg := range []TransitAlgorithm{TransitEd25519, TransitMLDSA65} {
		for _, size := range []int{256, MaxTransitPlaintextBytes} {
			msg := make([]byte, size)
			bind := goldenBinding(alg)
			material := goldenMaterial()
			k, err := OpenTransitKey(material, bind)
			if err != nil {
				b.Fatal(err)
			}
			pub, _ := k.PublicKey()
			payload, err := k.Sign(msg)
			if err != nil {
				b.Fatal(err)
			}
			name := string(alg) + "/msg=" + strconv.Itoa(size)
			if size == 256 {
				b.Run("open/"+string(alg), func(b *testing.B) {
					for b.Loop() {
						k, err := OpenTransitKey(material, bind)
						if err != nil {
							b.Fatal(err)
						}
						k.Destroy()
					}
				})
			}
			b.Run("sign-with-open/"+name, func(b *testing.B) {
				b.ReportMetric(float64(len(pub)), "pubkey-bytes")
				b.ReportMetric(float64(len(FormatTransitValue(1, payload))), "wire-chars")
				for b.Loop() {
					k, err := OpenTransitKey(material, bind)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := k.Sign(msg); err != nil {
						b.Fatal(err)
					}
					k.Destroy()
				}
			})
			b.Run("verify/"+name, func(b *testing.B) {
				for b.Loop() {
					if !VerifyTransitSignature(alg, pub, msg, payload) {
						b.Fatal("verify failed")
					}
				}
			})
		}
	}
}
