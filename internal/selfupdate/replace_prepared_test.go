package selfupdate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func TestReplaceWithPreparedInstallsOnlyAuthenticatedBytes(t *testing.T) {
	installer, status, installed, _, _, _ := preparedStableFixture(t)
	prepared, err := installer.PrepareRelease(t.Context(), status)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(installed)
	if err != nil {
		t.Fatal(err)
	}
	var authorized string
	if err := installer.ReplaceWithPrepared(t.Context(), prepared, func(path string) error { authorized = path; return nil }); err != nil {
		t.Fatal(err)
	}
	signed, err := os.ReadFile(prepared.BinaryPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(installed)
	if err != nil || string(got) != string(signed) || releaseidentity.Hash(got) != prepared.BinarySHA256 {
		t.Fatalf("installed executable = %q, %v; want the signed release bytes", got, err)
	}
	if info, err := os.Stat(installed); err != nil || info.Mode().Perm() != before.Mode().Perm() {
		t.Fatalf("installed mode = %v, %v; want the previous %v", info, err, before.Mode().Perm())
	}
	if resolved, err := filepath.EvalSymlinks(installed); err != nil || authorized != resolved {
		t.Fatalf("authorized %q, want the resolved running executable %q", authorized, resolved)
	}
}

func TestReplaceWithPreparedLeavesTheExecutableOnAnyRefusal(t *testing.T) {
	installer, status, installed, _, _, responses := preparedStableFixture(t)
	prepared, err := installer.PrepareRelease(t.Context(), status)
	if err != nil {
		t.Fatal(err)
	}
	unchanged := func(step string) {
		t.Helper()
		if got, err := os.ReadFile(installed); err != nil || string(got) != "old hikyo binary\n" {
			t.Fatalf("%s changed the executable: %q, %v", step, got, err)
		}
	}
	if err := installer.ReplaceWithPrepared(t.Context(), prepared, func(string) error { return errors.New("not root installed") }); err == nil {
		t.Fatal("replaced an executable that failed the authority check")
	}
	unchanged("authority refusal")
	// The cache is private, but the digest recorded at extraction still binds the file.
	if err := os.WriteFile(prepared.BinaryPath, []byte("unsigned executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := installer.ReplaceWithPrepared(t.Context(), prepared, func(string) error { return nil }); err == nil {
		t.Fatal("replaced the executable with bytes that differ from the verified digest")
	}
	unchanged("changed cached executable")
	installer.config.StateDir = t.TempDir()
	archive := "https://github.com/Hikyo-Org/Hikyo/releases/download/v1.0.0/" + mustArchiveName(t, "1.0.0")
	good := responses[archive]
	responses[archive] = []byte("tampered archive")
	if _, err := installer.PrepareRelease(t.Context(), status); err == nil {
		t.Fatal("prepared a tampered archive")
	}
	responses[archive] = good
	if _, err := installer.PrepareRelease(t.Context(), status); err != nil {
		t.Fatalf("control: the untampered archive must prepare: %v", err)
	}
	unchanged("tampered archive")
}
