package backup

import (
	"bytes"
	"io"
	"testing"
)

// FuzzBackupAuthentication exercises the policy around age, rather than only
// asking whether arbitrary containers crash. Failure may leave an unverified
// prefix in the scratch sink; callers must apply it only after a nil error.
func FuzzBackupAuthentication(f *testing.F) {
	identity, recipient, err := GenerateIdentity()
	if err != nil {
		f.Fatal(err)
	}
	otherIdentity, _, err := GenerateIdentity()
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte("backup payload"), uint64(0), byte(1))
	f.Add([]byte{}, uint64(100), byte(255))
	f.Add([]byte("chunk"), uint64(65536), byte(4))
	f.Fuzz(func(t *testing.T, plaintext []byte, index uint64, mask byte) {
		if len(plaintext) > 4096 {
			return
		}
		// Generate STREAM boundary payloads from small fuzz inputs. A large
		// seed makes the engine spend its minimization budget shrinking the
		// generator input rather than exercising authentication decisions.
		if index%32 == 0 && len(plaintext) > 0 {
			size := 65535 + int(index%3)
			plaintext = bytes.Repeat(plaintext, size/len(plaintext)+1)[:size]
		}
		var container bytes.Buffer
		writer, err := Encrypt(&container, Options{Recipients: []string{recipient}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(plaintext); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		raw := container.Bytes()
		var restored bytes.Buffer
		if err := ExtractTo(&restored, bytes.NewReader(raw), Unlock{Identity: identity}); err != nil || !bytes.Equal(restored.Bytes(), plaintext) {
			t.Fatalf("round-trip changed the archive: %v", err)
		}
		if err := ExtractTo(io.Discard, bytes.NewReader(raw), Unlock{Identity: otherIdentity}); err == nil {
			t.Fatal("wrong recipient authenticated the archive")
		}
		if err := ExtractTo(io.Discard, bytes.NewReader(raw[:index%uint64(len(raw))]), Unlock{Identity: identity}); err == nil {
			t.Fatal("proper prefix authenticated without a final chunk")
		}
		// Mutate the encrypted payload, avoiding syntactic header changes that
		// age may legitimately ignore (for example recipient stanza whitespace).
		tampered := bytes.Clone(raw)
		if mask == 0 {
			mask = 1
		}
		tampered[len(tampered)-1] ^= mask
		if err := ExtractTo(io.Discard, bytes.NewReader(tampered), Unlock{Identity: identity}); err == nil {
			t.Fatal("changed authentication tag authenticated")
		}
	})
}
