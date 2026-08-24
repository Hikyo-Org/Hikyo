package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareSocketRefusesToReplaceRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updater.sock")
	if err := os.WriteFile(path, []byte("operator data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareSocket(path); err == nil || !strings.Contains(err.Error(), "refuse to replace non-socket") {
		t.Fatalf("error = %v, want regular-file refusal", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "operator data" {
		t.Fatalf("regular file changed to %q", b)
	}
}
