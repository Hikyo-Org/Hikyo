package crypto

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/securefile"
)

// StageRootKey validates a group-readable projected Secret and atomically
// publishes an owner-only runtime file. Kubernetes applies fsGroup to Secret
// volumes, which makes the projected source readable by Hikyo's non-root UID;
// the server still receives the stricter 0400 file required by ReadRootKey.
func StageRootKey(source, destination string) error {
	raw, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read projected root key: %w", err)
	}
	defer Zero(raw)

	decoded, err := decodeRootKey(raw)
	if err != nil {
		return fmt.Errorf("validate projected root key: %w", err)
	}
	Zero(decoded)

	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create staged root-key directory: %w", err)
	}
	if err := securefile.WriteAtomic(destination, bytes.TrimSpace(raw), 0o400); err != nil {
		return fmt.Errorf("publish staged root key: %w", err)
	}
	return nil
}
