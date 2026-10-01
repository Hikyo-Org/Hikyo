//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionCustodyRejectsSymlinkAndReadableBearerFile(t *testing.T) {
	for _, kind := range []string{"symlink", "world-readable", "unsafe-directory"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			state := &State{dir: dir}
			if err := state.PutSession(SessionArtifact{Instance: "local", Token: "private-token"}); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink":
				private := filepath.Join(dir, "private-original")
				if err := os.Rename(state.sessionsPath(), private); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(private, state.sessionsPath()); err != nil {
					t.Fatal(err)
				}
			case "world-readable":
				if err := os.Chmod(state.sessionsPath(), 0o644); err != nil {
					t.Fatal(err)
				}
			case "unsafe-directory":
				if err := os.Chmod(dir, 0o777); err != nil {
					t.Fatal(err)
				}
			}
			if sessions, err := state.Sessions(); err == nil || sessions != nil {
				t.Fatalf("%s bearer state was accepted: %v", kind, err)
			}
		})
	}
}
