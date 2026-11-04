package backup

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// FuzzHybridExtractTo holds the reader rule against arbitrary containers: an
// archive opened with a hybrid identity never carries another stanza type.
func FuzzHybridExtractTo(f *testing.F) {
	identity, recipient, err := GenerateHybridIdentity()
	if err != nil {
		f.Fatal(err)
	}
	var container bytes.Buffer
	w, err := Encrypt(&container, Options{Recipients: []string{recipient}})
	if err != nil {
		f.Fatal(err)
	}
	if _, err := w.Write([]byte("backup payload")); err != nil {
		f.Fatal(err)
	}
	if err := w.Close(); err != nil {
		f.Fatal(err)
	}
	valid := container.Bytes()
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	mac := bytes.Index(valid, []byte("\n---"))
	f.Add(append(append(append([]byte{}, valid[:mac+1]...), "-> X25519 AAAA\nAAAA\n"...), valid[mac+1:]...))
	f.Add(append(append(append([]byte{}, valid[:mac+1]...), "-> scrypt AAAA 10\nAAAA\n"...), valid[mac+1:]...))

	f.Fuzz(func(t *testing.T, raw []byte) {
		err := ExtractTo(io.Discard, bytes.NewReader(raw), Unlock{Identity: identity})
		if err == nil && (bytes.Contains(raw, []byte("\n-> X25519")) || bytes.Contains(raw, []byte("\n-> scrypt"))) {
			t.Fatal("a container with a classical or scrypt stanza opened beside a hybrid one")
		}
	})
}

// FuzzRecipientPolicy holds the export rule against arbitrary recipient pairs:
// an accepted set is never a mix, and no input panics or echoes an identity.
func FuzzRecipientPolicy(f *testing.F) {
	_, hybrid, err := GenerateHybridIdentity()
	if err != nil {
		f.Fatal(err)
	}
	_, classical, err := GenerateIdentity()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(hybrid, classical)
	f.Add(hybrid, hybrid)
	f.Add(classical, "AGE-SECRET-KEY-PQ-1QQQ")
	f.Add(hybrid[:len(hybrid)-3], " "+hybrid)

	f.Fuzz(func(t *testing.T, a, b string) {
		err := Options{Recipients: []string{a, b}}.Validate()
		if err == nil && isHybridRecipient(a) != isHybridRecipient(b) {
			t.Fatal("a mixed recipient set was accepted")
		}
		if err != nil && strings.Contains(err.Error(), "AGE-SECRET-KEY") {
			t.Fatal("the error echoed an identity")
		}
	})
}
