package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestClientStateWritesIgnorePredictableTempSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions are a POSIX contract")
	}
	for _, tc := range []struct {
		name  string
		file  string
		write func(string) error
	}{
		{"sessions", "sessions.json", func(dir string) error {
			return (&State{dir: dir}).PutSession(SessionArtifact{Instance: "local", Token: "credential"})
		}},
		{"trust", "trust.json", func(dir string) error {
			return (&TrustStore{dir: dir}).Put(TrustEntry{Name: "local", Origin: "https://example.test"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "unrelated")
			original := []byte("untouched")
			if err := os.WriteFile(target, original, 0o600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, tc.file)
			if err := os.Symlink(target, path+".tmp"); err != nil {
				t.Fatal(err)
			}
			if err := tc.write(dir); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(after, original) {
				t.Fatalf("predictable temp symlink target changed: %q, %v", after, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("state mode = %o, want 600", info.Mode().Perm())
			}
		})
	}
}
